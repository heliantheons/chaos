package logquery

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/heliantheon/common/metric"
)

func TestQueryBuildsControlledLogQLAndNormalizesRecords(t *testing.T) {
	t.Parallel()
	var receivedQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedQuery = r.URL.Query().Get("query")
		w.Header().Set("Content-Type", "application/json")
		if _, err := io.WriteString(w, `{
          "status":"success",
          "data":{"resultType":"streams","result":[{
            "stream":{"namespace":"heliantheon-system","service":"chaos"},
            "values":[["1724360400000000000","{\"timestamp\":\"2024-08-22T21:00:00Z\",\"severity\":\"ERROR\",\"body\":\"delivery failed\",\"service.name\":\"chaos\",\"trace_id\":\"0123456789abcdef0123456789abcdef\"}"]]
          }]}}
        }`); err != nil {
			t.Errorf("write Loki response: %v", err)
		}
	}))
	defer server.Close()

	service := newTestService(t, server.URL)
	result, err := service.Query(context.Background(), Options{
		Service: "chaos", Severity: "ERROR", Search: `quote" | line_format "owned`,
		Start: time.Unix(1724360000, 0), End: time.Unix(1724361000, 0),
		Limit: 20, Direction: "backward",
	})
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}
	if !strings.Contains(receivedQuery, `|= "quote\" | line_format \"owned"`) {
		t.Fatalf("search was not safely quoted: %s", receivedQuery)
	}
	if !strings.Contains(receivedQuery, `service="chaos"`) || !strings.Contains(receivedQuery, `severity="ERROR"`) {
		t.Fatalf("expected controlled filters in query: %s", receivedQuery)
	}
	if len(result.Entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(result.Entries))
	}
	entry := result.Entries[0]
	if entry.Service != "chaos" || entry.Severity != "ERROR" || entry.Body != "delivery failed" {
		t.Fatalf("unexpected entry: %#v", entry)
	}
}

func TestParseOptionsRejectsUnboundedOrUnsafeQueries(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	tests := []url.Values{
		{"service": {`chaos"} |= "secret`}},
		{"trace_id": {"not-a-trace"}},
		{"limit": {"1001"}},
		{"start": {now.Add(-8 * 24 * time.Hour).Format(time.RFC3339)}, "end": {now.Format(time.RFC3339)}},
	}
	for _, values := range tests {
		if _, err := ParseOptions(values, now); err == nil {
			t.Fatalf("ParseOptions(%v) unexpectedly succeeded", values)
		}
	}
}

func newTestService(t *testing.T, endpoint string) *Service {
	t.Helper()
	metrics, err := metric.New(metric.Config{Service: "chaos"})
	if err != nil {
		t.Fatal(err)
	}
	service, err := New(endpoint, "heliantheon-system", metrics)
	if err != nil {
		t.Fatal(err)
	}
	return service
}
