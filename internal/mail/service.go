package mail

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/heliantheon/chaos/config"
	"github.com/heliantheon/chaos/internal/template"
	"github.com/heliantheon/common/eventbus"
	pkgmail "github.com/heliantheon/common/mail"
)

// Service queues mail requests and performs SMTP delivery in the worker.
type Service struct {
	client          *pkgmail.Client
	templateService *template.Service
	bus             *eventbus.Bus
	logger          *slog.Logger
	from            string
	fromName        string
}

// NewService creates the Chaos-owned mail service.
func NewService(templateService *template.Service, bus *eventbus.Bus, logger *slog.Logger) (*Service, error) {
	if templateService == nil || bus == nil || logger == nil {
		return nil, fmt.Errorf("mail: template service, event bus, and logger are required")
	}
	client, err := pkgmail.NewClient(&pkgmail.ClientConfig{
		Host:     config.GetSMTPHost(),
		Port:     config.GetSMTPPort(),
		Username: config.GetSMTPUsername(),
		Password: config.GetSMTPPassword(),
		UseSSL:   config.GetSMTPPort() == 465,
	})
	if err != nil {
		return nil, fmt.Errorf("create SMTP client: %w", err)
	}

	return &Service{
		client:          client,
		templateService: templateService,
		bus:             bus,
		logger:          logger,
		from:            config.GetSMTPFrom(),
		fromName:        config.GetSMTPFromName(),
	}, nil
}

// Enqueue publishes a private Chaos mail event and waits for JetStream PubAck.
func (s *Service) Enqueue(ctx context.Context, req SendRequest) (string, error) {
	deliveryID := uuid.NewString()
	variables := req.Variables
	if variables == nil {
		variables = req.Data
	}
	expiresAt := req.ExpiresAt
	if expiresAt.IsZero() {
		expiresAt = time.Now().Add(24 * time.Hour)
	}
	event := deliveryRequested{
		DeliveryID: deliveryID,
		To:         req.To,
		Subject:    req.Subject,
		TemplateID: req.TemplateID,
		Variables:  variables,
		ExpiresAt:  expiresAt.UTC(),
	}
	if _, err := s.bus.Publish(ctx, config.GetNATSSubject(), eventbus.Event{
		ID:      deliveryID,
		Type:    deliveryRequestedEventType,
		Subject: deliveryID,
		Data:    event,
	}); err != nil {
		return "", fmt.Errorf("queue mail delivery: %w", err)
	}

	s.logger.InfoContext(ctx, "mail delivery queued",
		"delivery_id", deliveryID,
		"template_id", req.TemplateID,
	)
	return deliveryID, nil
}

// HandleEvent decodes and delivers one private Chaos mail event.
func (s *Service) HandleEvent(ctx context.Context, message eventbus.Message) error {
	if message.Type != deliveryRequestedEventType {
		return eventbus.Permanent(fmt.Errorf("unsupported event type %q", message.Type))
	}
	var delivery deliveryRequested
	if err := message.Decode(&delivery); err != nil {
		return eventbus.Permanent(err)
	}
	if delivery.DeliveryID == "" || delivery.To == "" || delivery.TemplateID == "" || delivery.ExpiresAt.IsZero() {
		return eventbus.Permanent(fmt.Errorf("invalid mail delivery event"))
	}
	if !time.Now().Before(delivery.ExpiresAt) {
		s.logger.WarnContext(ctx, "mail delivery expired",
			"delivery_id", delivery.DeliveryID,
			"template_id", delivery.TemplateID,
		)
		return nil
	}
	return s.deliver(ctx, delivery)
}

// Verify validates SMTP connectivity without sending mail.
func (s *Service) Verify(ctx context.Context) error { return s.client.Verify(ctx) }

// Close closes the SMTP pool.
func (s *Service) Close() { s.client.Close() }

func (s *Service) deliver(ctx context.Context, delivery deliveryRequested) error {
	subject, body, err := s.templateService.Render(ctx, delivery.TemplateID, delivery.Variables)
	if err != nil {
		return fmt.Errorf("render mail template: %w", err)
	}
	if delivery.Subject != "" {
		subject = delivery.Subject
	}

	from := s.from
	if s.fromName != "" {
		from = fmt.Sprintf("%s <%s>", s.fromName, s.from)
	}
	message := pkgmail.NewMessage().
		SetFrom(from).
		AddTo(delivery.To).
		SetSubject(subject).
		SetHTML(body)

	if err := s.client.Send(ctx, message); err != nil {
		s.logger.ErrorContext(ctx, "mail delivery failed",
			"delivery_id", delivery.DeliveryID,
			"template_id", delivery.TemplateID,
			"error", err,
		)
		return fmt.Errorf("send mail: %w", err)
	}
	s.logger.InfoContext(ctx, "mail delivery completed",
		"delivery_id", delivery.DeliveryID,
		"template_id", delivery.TemplateID,
	)
	return nil
}
