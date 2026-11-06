package mail

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"

	"github.com/heliantheon/chaos/internal/models"
)

const (
	idempotencyLease = time.Minute
	statusPending    = "pending"
	statusRetryable  = "retryable"
	statusAccepted   = "accepted"
)

var (
	ErrIdempotencyConflict   = errors.New("idempotency key reused with a different request")
	ErrIdempotencyInProgress = errors.New("idempotent request is already in progress")
)

type deliveryClaim struct {
	DeliveryID string
	EventTime  time.Time
	ExpiresAt  time.Time
	Replay     bool
}

type idempotencyStore interface {
	Claim(context.Context, string, string, string, time.Time, time.Time) (deliveryClaim, error)
	MarkAccepted(context.Context, string, string) error
	MarkRetryable(context.Context, string, string) error
}

type GORMIdempotencyStore struct {
	db  *gorm.DB
	now func() time.Time
}

func NewGORMIdempotencyStore(db *gorm.DB) *GORMIdempotencyStore {
	return &GORMIdempotencyStore{db: db, now: time.Now}
}

func (s *GORMIdempotencyStore) Claim(
	ctx context.Context,
	keyHash string,
	requestHash string,
	deliveryID string,
	eventTime time.Time,
	expiresAt time.Time,
) (deliveryClaim, error) {
	if s == nil || s.db == nil {
		return deliveryClaim{}, errors.New("mail idempotency store is unavailable")
	}
	now := s.now().UTC()
	record := models.MailDeliveryRequest{
		IdempotencyKeyHash: keyHash,
		RequestHash:        requestHash,
		DeliveryID:         deliveryID,
		Status:             statusPending,
		EventTime:          eventTime.UTC(),
		ExpiresAt:          expiresAt.UTC(),
		LeaseUntil:         now.Add(idempotencyLease),
	}
	if err := s.db.WithContext(ctx).Create(&record).Error; err == nil {
		return claimFromRecord(record, false), nil
	}

	var existing models.MailDeliveryRequest
	if err := s.db.WithContext(ctx).
		Where("idempotency_key_hash = ?", keyHash).
		First(&existing).Error; err != nil {
		return deliveryClaim{}, fmt.Errorf("load mail idempotency record: %w", err)
	}
	if existing.RequestHash != requestHash {
		return deliveryClaim{}, ErrIdempotencyConflict
	}
	if existing.Status == statusAccepted {
		return claimFromRecord(existing, true), nil
	}

	result := s.db.WithContext(ctx).
		Model(&models.MailDeliveryRequest{}).
		Where(
			"idempotency_key_hash = ? AND request_hash = ? AND (status = ? OR (status = ? AND lease_until <= ?))",
			keyHash,
			requestHash,
			statusRetryable,
			statusPending,
			now,
		).
		Updates(map[string]any{
			"status":      statusPending,
			"lease_until": now.Add(idempotencyLease),
		})
	if result.Error != nil {
		return deliveryClaim{}, fmt.Errorf("reclaim mail idempotency record: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return deliveryClaim{}, ErrIdempotencyInProgress
	}
	return claimFromRecord(existing, false), nil
}

func (s *GORMIdempotencyStore) MarkAccepted(ctx context.Context, keyHash, deliveryID string) error {
	return s.updateStatus(ctx, keyHash, deliveryID, statusAccepted)
}

func (s *GORMIdempotencyStore) MarkRetryable(ctx context.Context, keyHash, deliveryID string) error {
	return s.updateStatus(ctx, keyHash, deliveryID, statusRetryable)
}

func (s *GORMIdempotencyStore) updateStatus(ctx context.Context, keyHash, deliveryID, status string) error {
	if s == nil || s.db == nil {
		return errors.New("mail idempotency store is unavailable")
	}
	result := s.db.WithContext(ctx).
		Model(&models.MailDeliveryRequest{}).
		Where("idempotency_key_hash = ? AND delivery_id = ?", keyHash, deliveryID).
		Updates(map[string]any{"status": status, "lease_until": s.now().UTC()})
	if result.Error != nil {
		return fmt.Errorf("update mail idempotency status: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return errors.New("mail idempotency record was not updated")
	}
	return nil
}

func claimFromRecord(record models.MailDeliveryRequest, replay bool) deliveryClaim {
	return deliveryClaim{
		DeliveryID: record.DeliveryID,
		EventTime:  record.EventTime,
		ExpiresAt:  record.ExpiresAt,
		Replay:     replay,
	}
}

func validateIdempotencyKey(key string) error {
	if key == "" {
		return fmt.Errorf("%w: Idempotency-Key is required", ErrInvalidRequest)
	}
	if len(key) > 200 || !utf8.ValidString(key) || strings.TrimSpace(key) != key {
		return fmt.Errorf("%w: Idempotency-Key is invalid", ErrInvalidRequest)
	}
	for _, r := range key {
		if r < 0x21 || r > 0x7e {
			return fmt.Errorf("%w: Idempotency-Key must contain visible ASCII characters", ErrInvalidRequest)
		}
	}
	return nil
}

func hashString(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
