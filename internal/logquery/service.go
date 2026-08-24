package logquery

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/heliantheon/common/metric"
)

const (
	defaultLimit    = 200
	maximumLimit    = 1000
	maximumRange    = 7 * 24 * time.Hour
	maximumBodySize = 16 << 20
)

var (
	servicePattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9.-]{0,61}[a-z0-9])?$`)
	traceIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)
	severities     = []string{"DEBUG", "INFO", "WARN", "ERROR"}
)

// Options is the public, constrained query surface. It deliberately contains
// no raw LogQL field.
type Options struct {
	Service     string
	Severity    string
	Environment string
	TraceID     string
	Search      string
	Start       time.Time
	End         time.Time
	Limit       int
	Direction   string
}

// Entry is a normalized log record returned to the Chaos UI.
type Entry struct {
	Timestamp   time.Time         `json:"timestamp"`
	TimestampNS string            `json:"timestamp_ns"`
	Service     string            `json:"service"`
	Severity    string            `json:"severity"`
	Body        string            `json:"body"`
	TraceID     string            `json:"trace_id,omitempty"`
	SpanID      string            `json:"span_id,omitempty"`
	Labels      map[string]string `json:"labels"`
	Attributes  map[string]any    `json:"attributes,omitempty"`
}

// Result contains a bounded page of log entries.
type Result struct {
	Entries []Entry   `json:"entries"`
	Start   time.Time `json:"start"`
	End     time.Time `json:"end"`
	Limit   int       `json:"limit"`
}

type Service struct {
	endpoint  *url.URL
	namespace string
	client    *http.Client
	queries   *prometheus.CounterVec
	duration  *prometheus.HistogramVec
}

func New(endpoint, namespace string, metrics *metric.Registry) (*Service, error) {
	parsed, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil {
		return nil, fmt.Errorf("parse Loki endpoint: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("loki endpoint must use http or https")
	}
	if parsed.Host == "" || metrics == nil {
		return nil, fmt.Errorf("loki endpoint and metrics are required")
	}
	namespace = strings.TrimSpace(namespace)
	if !servicePattern.MatchString(namespace) {
		return nil, fmt.Errorf("loki namespace is invalid")
	}
	queries, err := metrics.NewCounterVec(
		"log_queries_total",
		"Number of proxied Loki queries.",
		"result",
		"mode",
	)
	if err != nil {
		return nil, err
	}
	duration, err := metrics.NewHistogramVec(
		"log_query_duration_seconds",
		"Duration of proxied Loki queries.",
		[]float64{0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10},
		"mode",
	)
	if err != nil {
		return nil, err
	}
	return &Service{
		endpoint:  parsed,
		namespace: namespace,
		client:    &http.Client{Timeout: 12 * time.Second},
		queries:   queries,
		duration:  duration,
	}, nil
}

// ParseOptions validates HTTP query values without accepting raw LogQL.
func ParseOptions(values url.Values, now time.Time) (Options, error) {
	start, end, err := parseRange(values, now)
	if err != nil {
		return Options{}, err
	}
	service, severity, environment, traceID, search, err := parseFilters(values)
	if err != nil {
		return Options{}, err
	}
	limit, direction, err := parsePagination(values)
	if err != nil {
		return Options{}, err
	}

	return Options{
		Service: service, Severity: severity, Environment: environment,
		TraceID: traceID, Search: search, Start: start, End: end,
		Limit: limit, Direction: direction,
	}, nil
}

func parseRange(values url.Values, now time.Time) (time.Time, time.Time, error) {
	end, err := parseTime(values.Get("end"), now)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid end: %w", err)
	}
	start, err := parseTime(values.Get("start"), end.Add(-15*time.Minute))
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid start: %w", err)
	}
	if start.After(end) {
		return time.Time{}, time.Time{}, fmt.Errorf("start must not be after end")
	}
	if end.Sub(start) > maximumRange {
		return time.Time{}, time.Time{}, fmt.Errorf("query range must not exceed 7 days")
	}
	return start, end, nil
}

func parseFilters(values url.Values) (string, string, string, string, string, error) {
	service := strings.TrimSpace(values.Get("service"))
	if service != "" && !servicePattern.MatchString(service) {
		return "", "", "", "", "", fmt.Errorf("invalid service")
	}
	severity := strings.ToUpper(strings.TrimSpace(values.Get("severity")))
	if severity != "" && !slices.Contains(severities, severity) {
		return "", "", "", "", "", fmt.Errorf("invalid severity")
	}
	environment := strings.TrimSpace(values.Get("environment"))
	if len(environment) > 32 || strings.ContainsAny(environment, "\r\n\x00") {
		return "", "", "", "", "", fmt.Errorf("invalid environment")
	}
	traceID := strings.ToLower(strings.TrimSpace(values.Get("trace_id")))
	if traceID != "" && !traceIDPattern.MatchString(traceID) {
		return "", "", "", "", "", fmt.Errorf("trace_id must be 32 lowercase hexadecimal characters")
	}
	search := strings.TrimSpace(values.Get("search"))
	if len(search) > 256 || strings.ContainsAny(search, "\r\n\x00") {
		return "", "", "", "", "", fmt.Errorf("invalid search")
	}
	return service, severity, environment, traceID, search, nil
}

func parsePagination(values url.Values) (int, string, error) {
	limit := defaultLimit
	if rawLimit := values.Get("limit"); rawLimit != "" {
		parsedLimit, err := strconv.Atoi(rawLimit)
		limit = parsedLimit
		if err != nil || limit < 1 || limit > maximumLimit {
			return 0, "", fmt.Errorf("limit must be between 1 and %d", maximumLimit)
		}
	}
	direction := strings.ToLower(strings.TrimSpace(values.Get("direction")))
	if direction == "" {
		direction = "backward"
	}
	if direction != "forward" && direction != "backward" {
		return 0, "", fmt.Errorf("direction must be forward or backward")
	}
	return limit, direction, nil
}

func (s *Service) Query(ctx context.Context, options Options) (Result, error) {
	return s.query(ctx, options, "history")
}

// Tail polls Loki behind a server-sent event connection. Loki remains private
// and the caller controls cancellation through ctx.
func (s *Service) Tail(ctx context.Context, options Options, send func(Entry) error, heartbeat func() error) error {
	options.Direction = "forward"
	options.Limit = min(options.Limit, 200)
	cursor := options.Start
	seen := make(map[string]time.Time)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	lastHeartbeat := time.Now()

	for {
		options.Start = cursor
		options.End = time.Now().UTC()
		result, err := s.query(ctx, options, "tail")
		if err != nil {
			return err
		}
		nextCursor, err := sendNewEntries(result.Entries, cursor, seen, send)
		if err != nil {
			return err
		}
		// Keep a short overlap for logs that become queryable after ingestion,
		// while avoiding an ever-growing range for long-lived streams.
		if overlapCursor := options.End.Add(-5 * time.Second); overlapCursor.After(cursor) {
			cursor = overlapCursor
		} else {
			cursor = nextCursor
		}
		pruneSeen(seen, cursor.Add(-5*time.Second))
		if heartbeat != nil && time.Since(lastHeartbeat) >= 15*time.Second {
			if err := heartbeat(); err != nil {
				return err
			}
			lastHeartbeat = time.Now()
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (s *Service) query(ctx context.Context, options Options, mode string) (Result, error) {
	started := time.Now()
	result, err := s.queryLoki(ctx, options)
	s.duration.WithLabelValues(mode).Observe(time.Since(started).Seconds())
	if err != nil {
		s.queries.WithLabelValues("error", mode).Inc()
		return Result{}, err
	}
	s.queries.WithLabelValues("success", mode).Inc()
	return result, nil
}

func sendNewEntries(entries []Entry, cursor time.Time, seen map[string]time.Time, send func(Entry) error) (time.Time, error) {
	nextCursor := cursor
	for _, entry := range entries {
		key := entry.TimestampNS + "\x00" + entry.Service + "\x00" + entry.Labels["pod"] + "\x00" + entry.Body
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		if err := send(entry); err != nil {
			return cursor, err
		}
		if !entry.Timestamp.Before(nextCursor) {
			nextCursor = entry.Timestamp.Add(time.Nanosecond)
		}
		seen[key] = entry.Timestamp
	}
	return nextCursor, nil
}

func pruneSeen(seen map[string]time.Time, cutoff time.Time) {
	for key, timestamp := range seen {
		if timestamp.Before(cutoff) {
			delete(seen, key)
		}
	}
}

func (s *Service) queryLoki(ctx context.Context, options Options) (Result, error) {
	endpoint := s.endpoint.ResolveReference(&url.URL{Path: "/loki/api/v1/query_range"})
	values := endpoint.Query()
	values.Set("query", buildLogQL(s.namespace, options))
	values.Set("start", strconv.FormatInt(options.Start.UnixNano(), 10))
	values.Set("end", strconv.FormatInt(options.End.UnixNano(), 10))
	values.Set("limit", strconv.Itoa(options.Limit))
	values.Set("direction", options.Direction)
	endpoint.RawQuery = values.Encode()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return Result{}, fmt.Errorf("create Loki request: %w", err)
	}
	response, err := s.client.Do(request)
	if err != nil {
		return Result{}, fmt.Errorf("query Loki: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return Result{}, fmt.Errorf("loki returned %s", response.Status)
	}

	var payload lokiResponse
	decoder := json.NewDecoder(io.LimitReader(response.Body, maximumBodySize))
	if err := decoder.Decode(&payload); err != nil {
		return Result{}, fmt.Errorf("decode Loki response: %w", err)
	}
	if payload.Status != "success" || payload.Data.ResultType != "streams" {
		return Result{}, fmt.Errorf("unexpected Loki response")
	}

	entries := make([]Entry, 0)
	for _, stream := range payload.Data.Result {
		for _, value := range stream.Values {
			if len(value) != 2 {
				continue
			}
			timestampNS, err := strconv.ParseInt(value[0], 10, 64)
			if err != nil {
				continue
			}
			entries = append(entries, normalizeEntry(time.Unix(0, timestampNS).UTC(), value[0], stream.Stream, value[1]))
		}
	}
	slices.SortFunc(entries, func(a, b Entry) int {
		comparison := a.Timestamp.Compare(b.Timestamp)
		if options.Direction == "backward" {
			return -comparison
		}
		return comparison
	})
	if len(entries) > options.Limit {
		entries = entries[:options.Limit]
	}
	return Result{Entries: entries, Start: options.Start, End: options.End, Limit: options.Limit}, nil
}

func buildLogQL(namespace string, options Options) string {
	selector := `{namespace=` + strconv.Quote(namespace)
	if options.Service != "" {
		selector += `,service=` + strconv.Quote(options.Service)
	}
	selector += `}`
	if options.Search != "" {
		selector += ` |= ` + strconv.Quote(options.Search)
	}
	if options.Severity != "" || options.Environment != "" || options.TraceID != "" {
		selector += ` | json severity="severity", trace_id="trace_id", deployment_environment="deployment.environment"`
	}
	if options.Severity != "" {
		selector += ` | severity=` + strconv.Quote(options.Severity)
	}
	if options.Environment != "" {
		selector += ` | deployment_environment=` + strconv.Quote(options.Environment)
	}
	if options.TraceID != "" {
		selector += ` | trace_id=` + strconv.Quote(options.TraceID)
	}
	return selector
}

func normalizeEntry(timestamp time.Time, timestampNS string, labels map[string]string, line string) Entry {
	attributes := make(map[string]any)
	_ = json.Unmarshal([]byte(line), &attributes)
	entry := Entry{
		Timestamp: timestamp, TimestampNS: timestampNS,
		Service:  stringAttribute(attributes, "service.name", labels["service"]),
		Severity: stringAttribute(attributes, "severity", "UNKNOWN"),
		Body:     stringAttribute(attributes, "body", line),
		TraceID:  stringAttribute(attributes, "trace_id", ""),
		SpanID:   stringAttribute(attributes, "span_id", ""),
		Labels:   labels, Attributes: attributes,
	}
	return entry
}

func stringAttribute(attributes map[string]any, key, fallback string) string {
	if value, ok := attributes[key].(string); ok && value != "" {
		return value
	}
	return fallback
}

func parseTime(value string, fallback time.Time) (time.Time, error) {
	if strings.TrimSpace(value) == "" {
		return fallback.UTC(), nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, err
	}
	return parsed.UTC(), nil
}

type lokiResponse struct {
	Status string `json:"status"`
	Data   struct {
		ResultType string `json:"resultType"`
		Result     []struct {
			Stream map[string]string `json:"stream"`
			Values [][]string        `json:"values"`
		} `json:"result"`
	} `json:"data"`
}
