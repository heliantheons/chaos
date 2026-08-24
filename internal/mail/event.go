package mail

import "time"

const deliveryRequestedEventType = "com.heliantheon.chaos.mail.delivery.requested.v1"

// deliveryRequested is private to Chaos and must never move into common.
type deliveryRequested struct {
	DeliveryID string         `json:"delivery_id"`
	To         string         `json:"to"`
	Subject    string         `json:"subject,omitempty"`
	TemplateID string         `json:"template_id"`
	Variables  map[string]any `json:"variables"`
	ExpiresAt  time.Time      `json:"expires_at"`
}
