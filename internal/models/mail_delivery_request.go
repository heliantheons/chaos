package models

import "time"

// MailDeliveryRequest records the durable idempotency state of one accepted
// mail API intent. Idempotency keys are hashed before storage.
type MailDeliveryRequest struct {
	ID                 uint      `gorm:"primaryKey;autoIncrement;column:_id"`
	IdempotencyKeyHash string    `gorm:"column:idempotency_key_hash;size:64;not null;uniqueIndex"`
	RequestHash        string    `gorm:"column:request_hash;size:64;not null"`
	DeliveryID         string    `gorm:"column:delivery_id;size:36;not null;uniqueIndex"`
	Status             string    `gorm:"column:status;size:16;not null;index"`
	EventTime          time.Time `gorm:"column:event_time;not null"`
	ExpiresAt          time.Time `gorm:"column:expires_at;not null"`
	LeaseUntil         time.Time `gorm:"column:lease_until;not null;index"`
	CreatedAt          time.Time `gorm:"column:created_at;not null"`
	UpdatedAt          time.Time `gorm:"column:updated_at;not null"`
}

func (MailDeliveryRequest) TableName() string {
	return "t_mail_delivery_request"
}
