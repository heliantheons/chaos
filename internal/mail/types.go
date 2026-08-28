package mail

import (
	"fmt"
	"time"
)

// SendRequest is the public HTTP contract. SMTP details and rendered content
// are intentionally not accepted from callers.
type SendRequest struct {
	To         string         `json:"to" binding:"required,email"`
	Subject    string         `json:"subject,omitempty" binding:"max=998"`
	TemplateID string         `json:"template_id" binding:"required,max=64"`
	Variables  map[string]any `json:"variables,omitempty"`
	Data       map[string]any `json:"data,omitempty"`
	ExpiresAt  time.Time      `json:"expires_at,omitempty"`
}

// TemplateData returns the single normalized variable map used by both API
// validation and the asynchronous delivery consumer.
func (r SendRequest) TemplateData() (map[string]any, error) {
	if r.Variables != nil && r.Data != nil {
		return nil, fmt.Errorf("%w: variables and data cannot both be set", ErrInvalidRequest)
	}
	if r.Variables != nil {
		return r.Variables, nil
	}
	return r.Data, nil
}

// SendResponse is returned after the Knative Broker accepts the event.
type SendResponse struct {
	OK         bool   `json:"ok"`
	DeliveryID string `json:"delivery_id"`
}
