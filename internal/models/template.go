package models

import (
	"time"
)

// EmailTemplate 邮件模板
type EmailTemplate struct {
	ID          uint       `gorm:"primaryKey;autoIncrement;column:_id" json:"-"`
	TemplateID  string     `gorm:"column:template_id;size:64;not null;uniqueIndex" json:"template_id"`
	Name        string     `gorm:"column:name;size:128;not null" json:"name"`
	Description *string    `gorm:"column:description;size:512" json:"description,omitempty"`
	Subject     string     `gorm:"column:subject;size:256;not null" json:"subject"`
	Content     string     `gorm:"column:content;type:text;not null" json:"content"`
	Type        string     `gorm:"column:type;size:32;not null;default:'html'" json:"type"`
	Variables   *string    `gorm:"column:variables;type:text" json:"variables,omitempty"`
	ServiceID   *string    `gorm:"column:service_id;size:32;index" json:"service_id,omitempty"`
	IsBuiltin   bool       `gorm:"column:is_builtin;not null;default:false" json:"is_builtin"`
	IsEnabled   bool       `gorm:"column:is_enabled;not null;default:true" json:"is_enabled"`
	CreatedAt   time.Time  `gorm:"column:created_at;not null" json:"created_at"`
	UpdatedAt   time.Time  `gorm:"column:updated_at;not null" json:"updated_at"`
	DeletedAt   *time.Time `gorm:"column:deleted_at;index" json:"-"`
}

func (EmailTemplate) TableName() string {
	return "t_email_template"
}
