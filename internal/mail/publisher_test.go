package mail

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	cloudevents "github.com/cloudevents/sdk-go/v2"

	"github.com/heliantheon/chaos/internal/eventauth"
)

type recordingPublisher struct {
	event cloudevents.Event
	err   error
	calls int
}

func (p *recordingPublisher) Publish(_ context.Context, event cloudevents.Event) error {
	p.event = event
	p.calls++
	return p.err
}

type memoryIdempotencyRecord struct {
	claim       deliveryClaim
	requestHash string
	status      string
}

type memoryIdempotencyStore struct {
	records map[string]memoryIdempotencyRecord
}

func newMemoryIdempotencyStore() *memoryIdempotencyStore {
	return &memoryIdempotencyStore{records: make(map[string]memoryIdempotencyRecord)}
}

func (s *memoryIdempotencyStore) Claim(_ context.Context, keyHash, requestHash, deliveryID string, eventTime, expiresAt time.Time) (deliveryClaim, error) {
	record, ok := s.records[keyHash]
	if !ok {
		claim := deliveryClaim{DeliveryID: deliveryID, EventTime: eventTime, ExpiresAt: expiresAt}
		s.records[keyHash] = memoryIdempotencyRecord{claim: claim, requestHash: requestHash, status: statusPending}
		return claim, nil
	}
	if record.requestHash != requestHash {
		return deliveryClaim{}, ErrIdempotencyConflict
	}
	if record.status == statusAccepted {
		record.claim.Replay = true
		return record.claim, nil
	}
	if record.status == statusPending {
		return deliveryClaim{}, ErrIdempotencyInProgress
	}
	record.status = statusPending
	s.records[keyHash] = record
	return record.claim, nil
}

func (s *memoryIdempotencyStore) MarkAccepted(_ context.Context, keyHash, _ string) error {
	record := s.records[keyHash]
	record.status = statusAccepted
	s.records[keyHash] = record
	return nil
}

func (s *memoryIdempotencyStore) MarkRetryable(_ context.Context, keyHash, _ string) error {
	record := s.records[keyHash]
	record.status = statusRetryable
	s.records[keyHash] = record
	return nil
}

func TestPublisherSignsAndPublishesMailEvent(t *testing.T) {
	signer, err := eventauth.NewSigner(make([]byte, 48))
	if err != nil {
		t.Fatalf("NewSigner() error = %v", err)
	}
	bus := &recordingPublisher{}
	publisher, err := NewPublisher(bus, signer, newMemoryIdempotencyStore(), PublisherConfig{
		EventType: "com.heliantheon.chaos.mail.delivery.requested.v1",
		Source:    "urn:heliantheon:chaos:mail",
	}, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewPublisher() error = %v", err)
	}
	now := time.Date(2026, 8, 28, 10, 0, 0, 0, time.UTC)
	publisher.now = func() time.Time { return now }
	publisher.newID = func() string { return "5dd253af-178b-480d-a1dc-ab42cc5e4d6d" }

	id, err := publisher.Enqueue(t.Context(), "challenge-123", SendRequest{
		To:         "user@example.com",
		TemplateID: "otp_login",
		Variables:  map[string]any{"code": "123456"},
		ExpiresAt:  now.Add(5 * time.Minute),
	})
	if err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}
	if id != publisher.newID() {
		t.Fatalf("published id = %q", id)
	}
	if bus.event.ID() != id || bus.event.Subject() != id || bus.event.Type() != publisher.config.EventType {
		t.Fatalf("published event = %#v", bus.event)
	}

	var delivery deliveryRequested
	if err := signer.Verify(bus.event.Data(), eventauth.EventIdentity{
		ID:      bus.event.ID(),
		Type:    bus.event.Type(),
		Source:  publisher.config.Source,
		Subject: bus.event.Subject(),
	}, &delivery); err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if delivery.TemplateID != "otp_login" || delivery.Variables["code"] != "123456" {
		t.Fatalf("delivery = %#v", delivery)
	}
}

func TestPublisherRejectsInvalidRequestBeforePublishing(t *testing.T) {
	signer, err := eventauth.NewSigner(make([]byte, 48))
	if err != nil {
		t.Fatalf("NewSigner() error = %v", err)
	}
	bus := &recordingPublisher{}
	publisher, err := NewPublisher(bus, signer, newMemoryIdempotencyStore(), PublisherConfig{
		EventType: "com.heliantheon.chaos.mail.delivery.requested.v1",
		Source:    "urn:heliantheon:chaos:mail",
	}, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewPublisher() error = %v", err)
	}

	_, err = publisher.Enqueue(t.Context(), "challenge-123", SendRequest{To: "Display Name <user@example.com>", TemplateID: "otp_login"})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("Enqueue() error = %v, want ErrInvalidRequest", err)
	}
	if bus.event.Type() != "" {
		t.Fatal("invalid request was published")
	}
}

func TestPublisherReplaysAcceptedIdempotentRequestWithoutPublishingAgain(t *testing.T) {
	signer, err := eventauth.NewSigner(make([]byte, 48))
	if err != nil {
		t.Fatalf("NewSigner() error = %v", err)
	}
	bus := &recordingPublisher{}
	publisher, err := NewPublisher(bus, signer, newMemoryIdempotencyStore(), PublisherConfig{
		EventType: "com.heliantheon.chaos.mail.delivery.requested.v1",
		Source:    "urn:heliantheon:chaos:mail",
	}, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewPublisher() error = %v", err)
	}
	req := SendRequest{To: "user@example.com", TemplateID: "otp_login"}
	firstID, err := publisher.Enqueue(t.Context(), "challenge-123", req)
	if err != nil {
		t.Fatalf("first Enqueue() error = %v", err)
	}
	secondID, err := publisher.Enqueue(t.Context(), "challenge-123", req)
	if err != nil {
		t.Fatalf("second Enqueue() error = %v", err)
	}
	if firstID != secondID {
		t.Fatalf("delivery IDs differ: %q != %q", firstID, secondID)
	}
	if bus.calls != 1 {
		t.Fatalf("Publish() calls = %d, want 1", bus.calls)
	}
}

func TestPublisherRejectsIdempotencyKeyReusedWithDifferentRequest(t *testing.T) {
	signer, err := eventauth.NewSigner(make([]byte, 48))
	if err != nil {
		t.Fatalf("NewSigner() error = %v", err)
	}
	bus := &recordingPublisher{}
	publisher, err := NewPublisher(bus, signer, newMemoryIdempotencyStore(), PublisherConfig{
		EventType: "com.heliantheon.chaos.mail.delivery.requested.v1",
		Source:    "urn:heliantheon:chaos:mail",
	}, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewPublisher() error = %v", err)
	}
	if _, err := publisher.Enqueue(t.Context(), "challenge-123", SendRequest{To: "first@example.com", TemplateID: "otp_login"}); err != nil {
		t.Fatalf("first Enqueue() error = %v", err)
	}
	_, err = publisher.Enqueue(t.Context(), "challenge-123", SendRequest{To: "second@example.com", TemplateID: "otp_login"})
	if !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("second Enqueue() error = %v, want ErrIdempotencyConflict", err)
	}
}

func TestValidateDeliveryRejectsOversizedVariables(t *testing.T) {
	_, err := newDelivery(SendRequest{
		To:         "user@example.com",
		TemplateID: "otp_login",
		Variables:  map[string]any{"value": strings.Repeat("x", maxVariablesBytes)},
	}, "5dd253af-178b-480d-a1dc-ab42cc5e4d6d", time.Now())
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("newDelivery() error = %v, want ErrInvalidRequest", err)
	}
}

func TestValidateDeliveryBindsCloudEventIdentity(t *testing.T) {
	delivery := deliveryRequested{
		DeliveryID: "5dd253af-178b-480d-a1dc-ab42cc5e4d6d",
		To:         "user@example.com",
		TemplateID: "otp_login",
		ExpiresAt:  time.Now().Add(time.Minute),
	}
	event := cloudevents.NewEvent()
	event.SetID("different-id")
	event.SetSource("urn:test")
	event.SetType("test")
	event.SetSubject(delivery.DeliveryID)
	err := validateDelivery(event, delivery)
	if err == nil {
		t.Fatal("validateDelivery() error = nil")
	}
}

func TestValidateDeliveryRejectsSubjectHeaderInjection(t *testing.T) {
	_, err := newDelivery(SendRequest{
		To:         "user@example.com",
		TemplateID: "otp_login",
		Subject:    "OTP\r\nBcc: attacker@example.com",
	}, "5dd253af-178b-480d-a1dc-ab42cc5e4d6d", time.Now())
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("newDelivery() error = %v, want ErrInvalidRequest", err)
	}
}

func TestValidateDeliveryRejectsAmbiguousVariables(t *testing.T) {
	_, err := newDelivery(SendRequest{
		To:         "user@example.com",
		TemplateID: "otp_login",
		Variables:  map[string]any{"code": "123456"},
		Data:       map[string]any{"code": "654321"},
	}, "5dd253af-178b-480d-a1dc-ab42cc5e4d6d", time.Now())
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("newDelivery() error = %v, want ErrInvalidRequest", err)
	}
}
