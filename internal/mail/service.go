package mail

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	cloudevents "github.com/cloudevents/sdk-go/v2"

	"github.com/heliantheon/chaos/config"
	"github.com/heliantheon/chaos/internal/eventauth"
	"github.com/heliantheon/chaos/internal/template"
	pkgmail "github.com/heliantheon/common/mail"
)

// Service verifies private mail events and performs SMTP delivery.
type Service struct {
	client          *pkgmail.Client
	templateService *template.Service
	signer          *eventauth.Signer
	logger          *slog.Logger
	from            string
	fromName        string
	eventType       string
	eventSource     string
}

// NewService creates the Chaos-owned mail service.
func NewService(templateService *template.Service, signer *eventauth.Signer, eventType, eventSource string, logger *slog.Logger) (*Service, error) {
	if templateService == nil || signer == nil || logger == nil {
		return nil, fmt.Errorf("mail: template service, event signer, and logger are required")
	}
	if strings.TrimSpace(eventType) == "" || strings.TrimSpace(eventSource) == "" {
		return nil, fmt.Errorf("mail: event type and source are required")
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
		signer:          signer,
		logger:          logger,
		from:            config.GetSMTPFrom(),
		fromName:        config.GetSMTPFromName(),
		eventType:       eventType,
		eventSource:     eventSource,
	}, nil
}

// HandleEvent authenticates, validates, and delivers one private mail event.
func (s *Service) HandleEvent(ctx context.Context, event cloudevents.Event) error {
	if event.SpecVersion() != cloudevents.VersionV1 {
		return fmt.Errorf("unsupported CloudEvent specversion %q", event.SpecVersion())
	}
	if err := event.Validate(); err != nil {
		return fmt.Errorf("invalid CloudEvent: %w", err)
	}
	if event.Type() != s.eventType || event.Source() != s.eventSource {
		return fmt.Errorf("unexpected CloudEvent route")
	}
	if event.DataContentType() != cloudevents.ApplicationJSON {
		return fmt.Errorf("unsupported CloudEvent datacontenttype %q", event.DataContentType())
	}
	var delivery deliveryRequested
	if err := s.signer.Verify(event.Data(), eventauth.EventIdentity{
		ID:      event.ID(),
		Type:    event.Type(),
		Source:  event.Source(),
		Subject: event.Subject(),
	}, &delivery); err != nil {
		return err
	}
	if err := validateDelivery(event, delivery); err != nil {
		return err
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
