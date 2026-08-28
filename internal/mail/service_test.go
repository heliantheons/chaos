package mail

import (
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	"github.com/heliantheon/chaos/internal/eventauth"
)

func TestHandleEventAcknowledgesValidExpiredDelivery(t *testing.T) {
	signer, err := eventauth.NewSigner(make([]byte, 48))
	if err != nil {
		t.Fatalf("NewSigner() error = %v", err)
	}
	deliveryID := "5dd253af-178b-480d-a1dc-ab42cc5e4d6d"
	event := cloudevents.NewEvent()
	event.SetID(deliveryID)
	event.SetType("com.heliantheon.chaos.mail.delivery.requested.v1")
	event.SetSource("urn:heliantheon:chaos:mail")
	event.SetSubject(deliveryID)
	signed, err := signer.Sign(eventauth.EventIdentity{
		ID:      event.ID(),
		Type:    event.Type(),
		Source:  event.Source(),
		Subject: event.Subject(),
	}, deliveryRequested{
		DeliveryID: deliveryID,
		To:         "user@example.com",
		TemplateID: "otp_login",
		ExpiresAt:  time.Now().Add(-time.Minute),
	})
	if err != nil {
		t.Fatalf("Sign() error = %v", err)
	}
	if err := event.SetData(cloudevents.ApplicationJSON, signed); err != nil {
		t.Fatalf("SetData() error = %v", err)
	}

	service := &Service{signer: signer, logger: slog.New(slog.DiscardHandler), eventType: event.Type(), eventSource: event.Source()}
	if err := service.HandleEvent(t.Context(), event); err != nil {
		t.Fatalf("HandleEvent() error = %v", err)
	}
}

func TestHandleEventRejectsAlteredCloudEventMetadata(t *testing.T) {
	signer, err := eventauth.NewSigner(make([]byte, 48))
	if err != nil {
		t.Fatalf("NewSigner() error = %v", err)
	}
	deliveryID := "5dd253af-178b-480d-a1dc-ab42cc5e4d6d"
	identity := eventauth.EventIdentity{
		ID:      deliveryID,
		Type:    "com.heliantheon.chaos.mail.delivery.requested.v1",
		Source:  "urn:heliantheon:chaos:mail",
		Subject: deliveryID,
	}
	signed, err := signer.Sign(identity, deliveryRequested{
		DeliveryID: deliveryID,
		To:         "user@example.com",
		TemplateID: "otp_login",
		ExpiresAt:  time.Now().Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("Sign() error = %v", err)
	}
	raw, err := json.Marshal(signed)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	event := cloudevents.NewEvent()
	event.SetID(deliveryID)
	event.SetType(identity.Type)
	event.SetSource("urn:untrusted")
	event.SetSubject(deliveryID)
	if err := event.SetData(cloudevents.ApplicationJSON, json.RawMessage(raw)); err != nil {
		t.Fatalf("SetData() error = %v", err)
	}
	service := &Service{signer: signer, logger: slog.New(slog.DiscardHandler), eventType: identity.Type, eventSource: identity.Source}
	err = service.HandleEvent(t.Context(), event)
	if err == nil {
		t.Fatal("HandleEvent() error = nil")
	}
}
