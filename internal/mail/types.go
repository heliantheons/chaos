package mail

import "time"

// SendRequest is the public HTTP contract. SMTP details and rendered content
// are intentionally not accepted from callers.
type SendRequest struct {
	To         string         `json:"to" binding:"required,email"`
	Subject    string         `json:"subject,omitempty" binding:"max=998"`
	TemplateID string         `json:"template_id" binding:"required,max=128"`
	Variables  map[string]any `json:"variables,omitempty"`
	Data       map[string]any `json:"data,omitempty"`
	ExpiresAt  time.Time      `json:"expires_at,omitempty"`
}

// SendResponse is returned after JetStream confirms durable storage.
type SendResponse struct {
	DeliveryID string `json:"delivery_id"`
}
