package eventauth

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"

	pasetokit "github.com/heliantheon/aegis-go/utilities/paseto"
)

const (
	algorithm       = "hmac-sha256"
	derivationLabel = "chaos:event-signing:v1"
)

// Signer authenticates private Chaos event payloads with a purpose-derived
// key. The service seed is never used as an HMAC key directly.
type Signer struct {
	key []byte
}

// EventIdentity binds a signed payload to its CloudEvent routing metadata.
type EventIdentity struct {
	ID      string
	Type    string
	Source  string
	Subject string
}

// SignedPayload is the private envelope stored as CloudEvent data.
type SignedPayload struct {
	Algorithm    string          `json:"algorithm"`
	EventID      string          `json:"event_id"`
	EventType    string          `json:"event_type"`
	EventSource  string          `json:"event_source"`
	EventSubject string          `json:"event_subject"`
	Data         json.RawMessage `json:"data"`
	Signature    string          `json:"signature"`
}

type signedContent struct {
	Algorithm    string          `json:"algorithm"`
	EventID      string          `json:"event_id"`
	EventType    string          `json:"event_type"`
	EventSource  string          `json:"event_source"`
	EventSubject string          `json:"event_subject"`
	Data         json.RawMessage `json:"data"`
}

func NewSigner(serviceSeed []byte) (*Signer, error) {
	seed, err := pasetokit.ParseSeed(serviceSeed)
	if err != nil {
		return nil, fmt.Errorf("eventauth: parse service seed: %w", err)
	}
	return &Signer{key: seed.Derive(derivationLabel)}, nil
}

// Sign serializes data exactly once and signs those serialized bytes.
func (s *Signer) Sign(identity EventIdentity, data any) (SignedPayload, error) {
	if s == nil || len(s.key) == 0 {
		return SignedPayload{}, fmt.Errorf("eventauth: signer is not initialized")
	}
	if err := validateIdentity(identity); err != nil {
		return SignedPayload{}, err
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return SignedPayload{}, fmt.Errorf("eventauth: encode payload: %w", err)
	}

	content := signedContent{
		Algorithm:    algorithm,
		EventID:      identity.ID,
		EventType:    identity.Type,
		EventSource:  identity.Source,
		EventSubject: identity.Subject,
		Data:         raw,
	}
	signature, err := s.signContent(content)
	if err != nil {
		return SignedPayload{}, err
	}
	return SignedPayload{
		Algorithm:    content.Algorithm,
		EventID:      content.EventID,
		EventType:    content.EventType,
		EventSource:  content.EventSource,
		EventSubject: content.EventSubject,
		Data:         content.Data,
		Signature:    signature,
	}, nil
}

// Verify authenticates the raw payload before decoding business data.
func (s *Signer) Verify(raw []byte, identity EventIdentity, target any) error {
	if s == nil || len(s.key) == 0 {
		return fmt.Errorf("eventauth: signer is not initialized")
	}
	if target == nil {
		return fmt.Errorf("eventauth: decode target is nil")
	}

	var envelope SignedPayload
	if err := decodeStrict(raw, &envelope); err != nil {
		return fmt.Errorf("eventauth: decode signed payload: %w", err)
	}
	if envelope.Algorithm != algorithm {
		return fmt.Errorf("eventauth: unsupported algorithm %q", envelope.Algorithm)
	}
	if err := validateIdentity(identity); err != nil {
		return err
	}
	if envelope.EventID != identity.ID ||
		envelope.EventType != identity.Type ||
		envelope.EventSource != identity.Source ||
		envelope.EventSubject != identity.Subject {
		return fmt.Errorf("eventauth: signed event identity mismatch")
	}
	if len(envelope.Data) == 0 {
		return fmt.Errorf("eventauth: payload data is empty")
	}

	signature, err := base64.RawURLEncoding.DecodeString(envelope.Signature)
	if err != nil {
		return fmt.Errorf("eventauth: decode signature: %w", err)
	}
	content := signedContent{
		Algorithm:    envelope.Algorithm,
		EventID:      envelope.EventID,
		EventType:    envelope.EventType,
		EventSource:  envelope.EventSource,
		EventSubject: envelope.EventSubject,
		Data:         envelope.Data,
	}
	want, err := s.signContent(content)
	if err != nil {
		return err
	}
	wantSignature, err := base64.RawURLEncoding.DecodeString(want)
	if err != nil {
		return fmt.Errorf("eventauth: decode expected signature: %w", err)
	}
	if !hmac.Equal(signature, wantSignature) {
		return fmt.Errorf("eventauth: signature mismatch")
	}

	if err := decodeStrict(envelope.Data, target); err != nil {
		return fmt.Errorf("eventauth: decode verified data: %w", err)
	}
	return nil
}

func (s *Signer) signContent(content signedContent) (string, error) {
	raw, err := json.Marshal(content)
	if err != nil {
		return "", fmt.Errorf("eventauth: encode signed content: %w", err)
	}
	mac := hmac.New(sha256.New, s.key)
	_, _ = mac.Write(raw)
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func validateIdentity(identity EventIdentity) error {
	if identity.ID == "" || identity.Type == "" || identity.Source == "" || identity.Subject == "" {
		return fmt.Errorf("eventauth: complete event identity is required")
	}
	return nil
}

func decodeStrict(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return fmt.Errorf("unexpected trailing JSON value")
		}
		return err
	}
	return nil
}
