package template

import (
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/heliantheon/chaos/internal/models"
)

func TestRenderRequiresEveryReferencedVariable(t *testing.T) {
	service := &Service{logger: slog.New(slog.DiscardHandler)}
	tpl := &models.EmailTemplate{
		TemplateID: "otp_login",
		Subject:    "验证码 {{.Code}}",
		Content:    `<strong>{{.Code}}</strong><span>{{.ExpiresInMinutes}}</span>`,
	}

	_, _, err := service.render(t.Context(), tpl, map[string]any{"Code": "123456"})
	if !errors.Is(err, ErrDataMismatch) {
		t.Fatalf("render() error = %v, want ErrDataMismatch", err)
	}
}

func TestRenderRejectsMissingSubjectVariable(t *testing.T) {
	service := &Service{logger: slog.New(slog.DiscardHandler)}
	tpl := &models.EmailTemplate{
		TemplateID: "otp_login",
		Subject:    "验证码 {{.Code}}",
		Content:    `<p>登录验证</p>`,
	}

	_, _, err := service.render(t.Context(), tpl, map[string]any{})
	if !errors.Is(err, ErrDataMismatch) {
		t.Fatalf("render() error = %v, want ErrDataMismatch", err)
	}
}

func TestRenderReturnsAlignedSubjectAndBody(t *testing.T) {
	service := &Service{logger: slog.New(slog.DiscardHandler)}
	tpl := &models.EmailTemplate{
		TemplateID: "otp_login",
		Subject:    "验证码 {{.Code}}",
		Content:    `<strong>{{.Code}}</strong>`,
	}

	subject, body, err := service.render(t.Context(), tpl, map[string]any{"Code": "123456"})
	if err != nil {
		t.Fatalf("render() error = %v", err)
	}
	if subject != "验证码 123456" || body != "<strong>123456</strong>" {
		t.Fatalf("render() = (%q, %q)", subject, body)
	}
}

func TestRenderRejectsUnsafeRenderedSubject(t *testing.T) {
	service := &Service{logger: slog.New(slog.DiscardHandler)}
	tpl := &models.EmailTemplate{
		TemplateID: "otp_login",
		Subject:    "验证码 {{.Code}}",
		Content:    `<strong>{{.Code}}</strong>`,
	}

	_, _, err := service.render(t.Context(), tpl, map[string]any{"Code": "123456\r\nBcc: attacker@example.com"})
	if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "line break") {
		t.Fatalf("render() error = %v, want unsafe subject error", err)
	}
}
