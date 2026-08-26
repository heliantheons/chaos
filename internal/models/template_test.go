package models

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestEmailTemplateJSONContract(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 26, 12, 0, 0, 0, time.UTC)
	encoded, err := json.Marshal(EmailTemplate{
		ID:         42,
		TemplateID: "login_code",
		Name:       "Login code",
		Subject:    "Code",
		Content:    "<p>{{.Code}}</p>",
		Type:       "html",
		IsEnabled:  true,
		CreatedAt:  now,
		UpdatedAt:  now,
		DeletedAt:  &now,
	})
	if err != nil {
		t.Fatalf("marshal email template: %v", err)
	}

	payload := string(encoded)
	for _, key := range []string{
		`"template_id":"login_code"`,
		`"is_builtin":false`,
		`"is_enabled":true`,
		`"created_at":`,
		`"updated_at":`,
	} {
		if !strings.Contains(payload, key) {
			t.Errorf("JSON response %s does not contain %s", payload, key)
		}
	}
	for _, key := range []string{`"ID":`, `"TemplateID":`, `"DeletedAt":`, `"_id":`} {
		if strings.Contains(payload, key) {
			t.Errorf("JSON response %s exposes internal key %s", payload, key)
		}
	}
}
