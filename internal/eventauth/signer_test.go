package eventauth

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSignerRoundTrip(t *testing.T) {
	signer, err := NewSigner(make([]byte, 48))
	if err != nil {
		t.Fatalf("NewSigner() error = %v", err)
	}
	want := struct {
		ID string `json:"id"`
	}{ID: "delivery-1"}
	identity := EventIdentity{
		ID:      "delivery-1",
		Type:    "mail.requested.v1",
		Source:  "urn:heliantheon:chaos",
		Subject: "delivery-1",
	}

	signed, err := signer.Sign(identity, want)
	if err != nil {
		t.Fatalf("Sign() error = %v", err)
	}
	raw, err := json.Marshal(signed)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	var got struct {
		ID string `json:"id"`
	}
	if err := signer.Verify(raw, identity, &got); err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if got != want {
		t.Fatalf("Verify() = %#v, want %#v", got, want)
	}
}

func TestSignerRejectsTamperedData(t *testing.T) {
	signer, err := NewSigner(make([]byte, 48))
	if err != nil {
		t.Fatalf("NewSigner() error = %v", err)
	}
	identity := EventIdentity{ID: "delivery-1", Type: "mail.requested.v1", Source: "urn:heliantheon:chaos", Subject: "delivery-1"}
	signed, err := signer.Sign(identity, map[string]string{"id": "delivery-1"})
	if err != nil {
		t.Fatalf("Sign() error = %v", err)
	}
	signed.Data = []byte(`{"id":"delivery-2"}`)
	raw, err := json.Marshal(signed)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	var got map[string]string
	if err := signer.Verify(raw, identity, &got); err == nil || !strings.Contains(err.Error(), "signature mismatch") {
		t.Fatalf("Verify() error = %v, want signature mismatch", err)
	}
}

func TestSignerRejectsUnknownEnvelopeFields(t *testing.T) {
	signer, err := NewSigner(make([]byte, 48))
	if err != nil {
		t.Fatalf("NewSigner() error = %v", err)
	}
	var got map[string]string
	identity := EventIdentity{ID: "delivery-1", Type: "mail.requested.v1", Source: "urn:heliantheon:chaos", Subject: "delivery-1"}
	err = signer.Verify([]byte(`{"algorithm":"hmac-sha256","event_id":"delivery-1","event_type":"mail.requested.v1","event_source":"urn:heliantheon:chaos","event_subject":"delivery-1","data":{},"signature":"x","extra":true}`), identity, &got)
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("Verify() error = %v, want unknown field", err)
	}
}

func TestSignerRejectsAlteredEventIdentity(t *testing.T) {
	signer, err := NewSigner(make([]byte, 48))
	if err != nil {
		t.Fatalf("NewSigner() error = %v", err)
	}
	identity := EventIdentity{ID: "delivery-1", Type: "mail.requested.v1", Source: "urn:heliantheon:chaos", Subject: "delivery-1"}
	signed, err := signer.Sign(identity, map[string]string{"id": "delivery-1"})
	if err != nil {
		t.Fatalf("Sign() error = %v", err)
	}
	raw, err := json.Marshal(signed)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	identity.Source = "urn:untrusted"
	var got map[string]string
	if err := signer.Verify(raw, identity, &got); err == nil || !strings.Contains(err.Error(), "identity mismatch") {
		t.Fatalf("Verify() error = %v, want identity mismatch", err)
	}
}
