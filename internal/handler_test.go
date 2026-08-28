package chaos

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/heliantheon/aegis-go/guard"
	tokendef "github.com/heliantheon/aegis-go/utilities/token"
	"github.com/heliantheon/chaos/internal/mail"
	mailtemplate "github.com/heliantheon/chaos/internal/template"
)

type stubMailEnqueuer struct {
	deliveryID string
	err        error
	key        string
}

func (s *stubMailEnqueuer) Enqueue(_ context.Context, key string, _ mail.SendRequest) (string, error) {
	s.key = key
	return s.deliveryID, s.err
}

type stubMailTemplateValidator struct {
	err error
}

func (s stubMailTemplateValidator) Validate(_ context.Context, _ string, _ map[string]any) error {
	return s.err
}

func TestDecodeMailRequestRejectsUnknownFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	request := httptest.NewRequestWithContext(t.Context(), "POST", "/api/mail", strings.NewReader(`{
		"to":"user@example.com",
		"template_id":"otp_login",
		"unexpected":true
	}`))
	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	context.Request = request

	var target mail.SendRequest
	if err := decodeMailRequest(context, &target); err == nil {
		t.Fatal("decodeMailRequest() error = nil")
	}
}

func TestDecodeMailRequestRejectsTrailingJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	request := httptest.NewRequestWithContext(t.Context(), "POST", "/api/mail", strings.NewReader(`{"to":"user@example.com","template_id":"otp_login"} {}`))
	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	context.Request = request

	var target mail.SendRequest
	if err := decodeMailRequest(context, &target); err == nil {
		t.Fatal("decodeMailRequest() error = nil")
	}
}

func TestSendMailReturnsAcceptedDelivery(t *testing.T) {
	gin.SetMode(gin.TestMode)
	enqueuer := &stubMailEnqueuer{deliveryID: "5dd253af-178b-480d-a1dc-ab42cc5e4d6d"}
	handler := &Handler{mailPublisher: enqueuer, mailValidator: stubMailTemplateValidator{}}
	request := httptest.NewRequestWithContext(t.Context(), "POST", "/api/mail", strings.NewReader(`{"to":"user@example.com","template_id":"otp_login"}`))
	request.Header.Set("Idempotency-Key", "challenge-123")
	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	context.Request = request

	handler.SendMail(context)

	if response.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusAccepted)
	}
	if enqueuer.key != "challenge-123" {
		t.Fatalf("idempotency key = %q", enqueuer.key)
	}
	if !strings.Contains(response.Body.String(), `"ok":true`) {
		t.Fatalf("response = %s, want ok=true", response.Body.String())
	}
}

func TestSendMailMapsIdempotencyConflict(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := &Handler{mailPublisher: &stubMailEnqueuer{err: mail.ErrIdempotencyConflict}, mailValidator: stubMailTemplateValidator{}}
	request := httptest.NewRequestWithContext(t.Context(), "POST", "/api/mail", strings.NewReader(`{"to":"user@example.com","template_id":"otp_login"}`))
	request.Header.Set("Idempotency-Key", "challenge-123")
	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	context.Request = request

	handler.SendMail(context)

	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnprocessableEntity)
	}
}

func TestRequireServiceAccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name       string
		access     tokendef.AccessToken
		wantStatus int
	}{
		{name: "service", access: &tokendef.ServiceAccessToken{}, wantStatus: http.StatusNoContent},
		{name: "user", access: &tokendef.UserAccessToken{}, wantStatus: http.StatusForbidden},
		{name: "missing", wantStatus: http.StatusForbidden},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := gin.New()
			router.Use(func(c *gin.Context) {
				if tt.access != nil {
					c.Request = c.Request.WithContext(guard.WithTokenContext(c.Request.Context(), &guard.TokenContext{AccessToken: tt.access}))
				}
				c.Next()
			})
			router.POST("/api/mail", requireServiceAccess(), func(c *gin.Context) { c.Status(http.StatusNoContent) })
			request := httptest.NewRequestWithContext(t.Context(), "POST", "/api/mail", nil)
			response := httptest.NewRecorder()

			router.ServeHTTP(response, request)

			if response.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", response.Code, tt.wantStatus)
			}
		})
	}
}

func TestSendMailMapsQueueFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := &Handler{mailPublisher: &stubMailEnqueuer{err: errors.New("broker unavailable")}, mailValidator: stubMailTemplateValidator{}}
	request := httptest.NewRequestWithContext(t.Context(), "POST", "/api/mail", strings.NewReader(`{"to":"user@example.com","template_id":"otp_login"}`))
	request.Header.Set("Idempotency-Key", "challenge-123")
	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	context.Request = request

	handler.SendMail(context)

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusServiceUnavailable)
	}
}

func TestSendMailRejectsUnknownTemplateBeforeEnqueue(t *testing.T) {
	gin.SetMode(gin.TestMode)
	enqueuer := &stubMailEnqueuer{deliveryID: "should-not-be-used"}
	handler := &Handler{
		mailPublisher: enqueuer,
		mailValidator: stubMailTemplateValidator{err: mailtemplate.ErrNotFound},
	}
	request := httptest.NewRequestWithContext(t.Context(), "POST", "/api/mail", strings.NewReader(`{"to":"user@example.com","template_id":"missing"}`))
	request.Header.Set("Idempotency-Key", "challenge-123")
	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	context.Request = request

	handler.SendMail(context)

	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNotFound)
	}
	if enqueuer.key != "" {
		t.Fatal("invalid template request was enqueued")
	}
}

func TestSendMailRejectsTemplateDataMismatchBeforeEnqueue(t *testing.T) {
	gin.SetMode(gin.TestMode)
	enqueuer := &stubMailEnqueuer{deliveryID: "should-not-be-used"}
	handler := &Handler{
		mailPublisher: enqueuer,
		mailValidator: stubMailTemplateValidator{err: mailtemplate.ErrDataMismatch},
	}
	request := httptest.NewRequestWithContext(t.Context(), "POST", "/api/mail", strings.NewReader(`{"to":"user@example.com","template_id":"otp_login","variables":{}}`))
	request.Header.Set("Idempotency-Key", "challenge-123")
	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	context.Request = request

	handler.SendMail(context)

	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnprocessableEntity)
	}
	if enqueuer.key != "" {
		t.Fatal("template data mismatch was enqueued")
	}
}
