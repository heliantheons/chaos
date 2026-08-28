package mail

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	"github.com/google/uuid"

	"github.com/heliantheon/chaos/internal/eventauth"
)

type EventPublisher interface {
	Publish(context.Context, cloudevents.Event) error
}

type PublisherConfig struct {
	EventType string
	Source    string
}

// Publisher is the HTTP-facing mail queue boundary. It never connects to SMTP.
type Publisher struct {
	bus    EventPublisher
	signer *eventauth.Signer
	store  idempotencyStore
	config PublisherConfig
	logger *slog.Logger
	now    func() time.Time
	newID  func() string
}

func NewPublisher(bus EventPublisher, signer *eventauth.Signer, store idempotencyStore, cfg PublisherConfig, logger *slog.Logger) (*Publisher, error) {
	if bus == nil || signer == nil || store == nil || logger == nil {
		return nil, fmt.Errorf("mail publisher: event bus, signer, idempotency store, and logger are required")
	}
	if strings.TrimSpace(cfg.EventType) == "" || strings.TrimSpace(cfg.Source) == "" {
		return nil, fmt.Errorf("mail publisher: event type and source are required")
	}
	return &Publisher{
		bus:    bus,
		signer: signer,
		store:  store,
		config: cfg,
		logger: logger,
		now:    time.Now,
		newID:  uuid.NewString,
	}, nil
}

// Enqueue signs a private mail event and waits for the Knative Broker ingress
// to acknowledge it.
func (p *Publisher) Enqueue(ctx context.Context, idempotencyKey string, req SendRequest) (string, error) {
	if err := validateIdempotencyKey(idempotencyKey); err != nil {
		return "", err
	}
	requestHash, err := hashSendRequest(req)
	if err != nil {
		return "", err
	}
	eventTime := p.now().UTC()
	delivery, err := newDelivery(req, p.newID(), eventTime)
	if err != nil {
		return "", err
	}
	keyHash := hashString(idempotencyKey)
	claim, err := p.store.Claim(
		ctx,
		keyHash,
		requestHash,
		delivery.DeliveryID,
		eventTime,
		delivery.ExpiresAt,
	)
	if err != nil {
		return "", err
	}
	if claim.Replay {
		return claim.DeliveryID, nil
	}
	if claim.DeliveryID != delivery.DeliveryID {
		req.ExpiresAt = claim.ExpiresAt
		delivery, err = newDelivery(req, claim.DeliveryID, claim.EventTime)
		if err != nil {
			return "", err
		}
		eventTime = claim.EventTime
	}
	signed, err := p.signer.Sign(eventauth.EventIdentity{
		ID:      delivery.DeliveryID,
		Type:    p.config.EventType,
		Source:  p.config.Source,
		Subject: delivery.DeliveryID,
	}, delivery)
	if err != nil {
		return "", fmt.Errorf("sign mail delivery event: %w", err)
	}
	event := cloudevents.NewEvent(cloudevents.VersionV1)
	event.SetID(delivery.DeliveryID)
	event.SetSource(p.config.Source)
	event.SetType(p.config.EventType)
	event.SetSubject(delivery.DeliveryID)
	event.SetTime(eventTime)
	if err := event.SetData(cloudevents.ApplicationJSON, signed); err != nil {
		return "", fmt.Errorf("encode mail delivery CloudEvent: %w", err)
	}
	if err := p.bus.Publish(ctx, event); err != nil {
		if storeErr := p.store.MarkRetryable(ctx, keyHash, delivery.DeliveryID); storeErr != nil {
			return "", fmt.Errorf("queue mail delivery: %w; mark retryable: %w", err, storeErr)
		}
		return "", fmt.Errorf("queue mail delivery: %w", err)
	}
	if err := p.store.MarkAccepted(ctx, keyHash, delivery.DeliveryID); err != nil {
		return "", fmt.Errorf("record accepted mail delivery: %w", err)
	}

	p.logger.InfoContext(ctx, "mail delivery queued",
		"delivery_id", delivery.DeliveryID,
		"template_id", delivery.TemplateID,
	)
	return delivery.DeliveryID, nil
}

func hashSendRequest(req SendRequest) (string, error) {
	variables, err := req.TemplateData()
	if err != nil {
		return "", err
	}
	canonical := struct {
		To         string         `json:"to"`
		Subject    string         `json:"subject,omitempty"`
		TemplateID string         `json:"template_id"`
		Variables  map[string]any `json:"variables,omitempty"`
		ExpiresAt  time.Time      `json:"expires_at,omitempty"`
	}{
		To:         strings.TrimSpace(req.To),
		Subject:    req.Subject,
		TemplateID: strings.TrimSpace(req.TemplateID),
		Variables:  variables,
		ExpiresAt:  req.ExpiresAt.UTC(),
	}
	raw, err := json.Marshal(canonical)
	if err != nil {
		return "", fmt.Errorf("%w: request is not JSON encodable", ErrInvalidRequest)
	}
	return hashString(string(raw)), nil
}
