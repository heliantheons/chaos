package mail

import (
	"encoding/json"
	"errors"
	"fmt"
	netmail "net/mail"
	"regexp"
	"strings"
	"time"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	"github.com/google/uuid"
)

const (
	defaultDeliveryTTL = 24 * time.Hour
	maxVariablesBytes  = 32 * 1024
)

var (
	ErrInvalidRequest = errors.New("invalid mail request")
	templateIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)
)

func newDelivery(req SendRequest, deliveryID string, now time.Time) (deliveryRequested, error) {
	variables, err := req.TemplateData()
	if err != nil {
		return deliveryRequested{}, err
	}
	to := strings.TrimSpace(req.To)
	templateID := strings.TrimSpace(req.TemplateID)
	expiresAt := req.ExpiresAt
	if expiresAt.IsZero() {
		expiresAt = now.Add(defaultDeliveryTTL)
	}
	delivery := deliveryRequested{
		DeliveryID: deliveryID,
		To:         to,
		Subject:    req.Subject,
		TemplateID: templateID,
		Variables:  variables,
		ExpiresAt:  expiresAt.UTC(),
	}
	if err := validateDeliveryData(delivery); err != nil {
		return deliveryRequested{}, fmt.Errorf("%w: %w", ErrInvalidRequest, err)
	}
	if !delivery.ExpiresAt.After(now) || delivery.ExpiresAt.After(now.Add(defaultDeliveryTTL)) {
		return deliveryRequested{}, fmt.Errorf("%w: expires_at must be within the next 24 hours", ErrInvalidRequest)
	}
	return delivery, nil
}

func validateDelivery(event cloudevents.Event, delivery deliveryRequested) error {
	if err := validateDeliveryData(delivery); err != nil {
		return err
	}
	if event.ID() != delivery.DeliveryID {
		return fmt.Errorf("mail delivery ID does not match CloudEvent ID")
	}
	if event.Subject() != delivery.DeliveryID {
		return fmt.Errorf("mail delivery ID does not match CloudEvent subject")
	}
	return nil
}

func validateDeliveryData(delivery deliveryRequested) error {
	if _, err := uuid.Parse(delivery.DeliveryID); err != nil {
		return fmt.Errorf("delivery_id is not a UUID")
	}
	address, err := netmail.ParseAddress(delivery.To)
	if err != nil || address.Address != delivery.To {
		return fmt.Errorf("to is not a plain email address")
	}
	if !templateIDPattern.MatchString(delivery.TemplateID) {
		return fmt.Errorf("template_id has an invalid format")
	}
	if len(delivery.Subject) > 998 {
		return fmt.Errorf("subject exceeds 998 characters")
	}
	if strings.ContainsAny(delivery.Subject, "\r\n") {
		return fmt.Errorf("subject contains a line break")
	}
	if delivery.ExpiresAt.IsZero() {
		return fmt.Errorf("expires_at is required")
	}
	raw, err := json.Marshal(delivery.Variables)
	if err != nil {
		return fmt.Errorf("variables are not JSON encodable")
	}
	if len(raw) > maxVariablesBytes {
		return fmt.Errorf("variables exceed %d bytes", maxVariablesBytes)
	}
	return nil
}
