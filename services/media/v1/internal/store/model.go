package store

import (
	"encoding/json"
	"fmt"
)

type Status string

const (
	StatusCreated    Status = "CREATED"
	StatusCompleting Status = "COMPLETING"
	StatusUploaded   Status = "UPLOADED"
	StatusProcessing Status = "PROCESSING"
	StatusReady      Status = "READY"
	StatusFailed     Status = "FAILED"
	StatusAborted    Status = "ABORTED"
	StatusExpired    Status = "EXPIRED"
)

func (s Status) Valid() bool {
	switch s {
	case StatusCreated, StatusCompleting, StatusUploaded,
		StatusProcessing, StatusReady, StatusFailed, StatusAborted, StatusExpired:
		return true
	}
	return false
}

func (s Status) Terminal() bool {
	return s == StatusFailed || s == StatusAborted || s == StatusExpired
}

type Kind string

const (
	KindUserAvatar        Kind = "user_avatar"
	KindGuildIcon         Kind = "guild_icon"
	KindMessageAttachment Kind = "message_attachment"
)

func (k Kind) Valid() bool {
	switch k {
	case KindUserAvatar, KindGuildIcon, KindMessageAttachment:
		return true
	}
	return false
}

func (k Kind) IsImage() bool {
	return k == KindUserAvatar || k == KindGuildIcon
}

type Variant struct {
	Key          string `json:"key"`
	MaxDimension int32  `json:"max_dimension"`
	Width        int32  `json:"width"`
	Height       int32  `json:"height"`
	Size         int64  `json:"size"`
}

type Asset struct {
	ID              int64  `db:"id"`
	CreatedByUserID int64  `db:"created_by_user_id"`
	SubjectID       int64  `db:"subject_id"`
	Kind            Kind   `db:"kind"`
	Status          Status `db:"status"`
	StorageBackend  string `db:"storage_backend"`
	StagingKey      string `db:"staging_key"`
	PublishedKey    string `db:"published_key"`
	ExpectedSize    int64  `db:"expected_size"`
	ActualSize      int64  `db:"actual_size"`
	ContentType     string `db:"content_type"`
	ExpiresAt       int64  `db:"expires_at"`
	Width           int32  `db:"width"`
	Height          int32  `db:"height"`
	VariantsJSON    string `db:"variants"`
	ErrorMessage    string `db:"error_message"`
	CreatedAt       int64  `db:"created_at"`
	UpdatedAt       int64  `db:"updated_at"`
	DeletedAt       int64  `db:"deleted_at"`
}

func (a *Asset) Variants() []Variant {
	if a.VariantsJSON == "" || a.VariantsJSON == "[]" {
		return nil
	}
	var v []Variant
	if err := json.Unmarshal([]byte(a.VariantsJSON), &v); err != nil {
		return nil
	}
	return v
}

func (a *Asset) SetVariants(v []Variant) {
	if v == nil {
		v = []Variant{}
	}
	data, _ := json.Marshal(v)
	a.VariantsJSON = string(data)
}

func (a *Asset) PublicPrefix() string {
	switch a.Kind {
	case KindUserAvatar:
		return fmt.Sprintf("avatars/%d/%d", a.SubjectID, a.ID)
	case KindGuildIcon:
		return fmt.Sprintf("icons/%d/%d", a.SubjectID, a.ID)
	default:
		return fmt.Sprintf("assets/%d", a.ID)
	}
}
