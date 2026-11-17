package repo

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm/clause"
)

// OpenCode is the platform's first executor. Users without their own OpenCode
// credential use the platform key configured under 平台管理, limited to the
// models the administrator allowed. Unlike CodeBuddy, OpenCode has no model
// service of its own, so the shared record carries the Base URL, protocol and
// model list as well as the key.
const (
	MaxSharedOpenCodeModels = 32

	OpenCodeProtocolChat      = "chat"
	OpenCodeProtocolResponses = "responses"
	OpenCodeProtocolMessages  = "messages"
)

// SharedOpenCodeKey is the credential users without their own borrow.
type SharedOpenCodeKey struct {
	APIKey   string
	BaseURL  string
	Protocol string
	// Models are the models it may run; the first is the default.
	Models []string
}

// Model returns the model a shared-key turn runs: the requested model when it
// is allowed, the default when none is requested, and false otherwise.
func (k SharedOpenCodeKey) Model(requested string) (string, bool) {
	if len(k.Models) == 0 {
		return "", false
	}
	requested = strings.TrimSpace(requested)
	if requested == "" {
		return k.Models[0], true
	}
	for _, id := range k.Models {
		if id == requested {
			return id, true
		}
	}
	return "", false
}

// ProtocolOrDefault normalizes an empty or unknown protocol to chat.
func (k SharedOpenCodeKey) ProtocolOrDefault() string {
	switch strings.ToLower(strings.TrimSpace(k.Protocol)) {
	case OpenCodeProtocolResponses:
		return OpenCodeProtocolResponses
	case OpenCodeProtocolMessages:
		return OpenCodeProtocolMessages
	default:
		return OpenCodeProtocolChat
	}
}

// The key is stored like personal agent credentials: the database is the trust
// boundary, and the key is never returned by the API.
type platformOpenCodeKeyRow struct {
	ID        int64      `gorm:"column:id;primaryKey;autoIncrement:false"`
	APIKey    string     `gorm:"column:api_key;type:text;not null"`
	BaseURL   string     `gorm:"column:base_url;type:varchar(2048);not null;default:''"`
	Protocol  string     `gorm:"column:protocol;type:varchar(32);not null;default:'chat'"`
	Models    string     `gorm:"column:models;type:text;not null"`
	UpdatedBy int64      `gorm:"column:updated_by;not null;default:0"`
	Updated   *time.Time `gorm:"column:updated_at"`
}

func (platformOpenCodeKeyRow) TableName() string { return "platform_opencode_keys" }

// decodeSharedModels parses the stored JSON model allow list. A malformed or
// empty list yields nil, which callers treat as "no shared key".
func decodeSharedModels(raw string) []string {
	var models []string
	if strings.TrimSpace(raw) == "" || json.Unmarshal([]byte(raw), &models) != nil {
		return nil
	}
	return models
}

const platformOpenCodeKeyID = 1

// PlatformOpenCodeKeyStatus is what administrators see; it never carries the
// key.
type PlatformOpenCodeKeyStatus struct {
	Configured bool       `json:"configured"`
	BaseURL    string     `json:"baseUrl"`
	Protocol   string     `json:"protocol"`
	Models     []string   `json:"models"`
	UpdatedAt  *time.Time `json:"updatedAt,omitempty"`
	UpdatedBy  string     `json:"updatedBy,omitempty"`
}

// PlatformOpenCodeKeyUpdate replaces the shared key settings. An empty APIKey
// keeps the stored key. Models and BaseURL must already be validated.
type PlatformOpenCodeKeyUpdate struct {
	APIKey   string
	BaseURL  string
	Protocol string
	Models   []string
}

var (
	ErrSharedOpenCodeKeyRequired    = errors.New("shared OpenCode API Key is required")
	ErrSharedOpenCodeModelsRequired = errors.New("at least one shared OpenCode model is required")
)

func (r *UsersRepo) platformOpenCodeKey() (platformOpenCodeKeyRow, bool) {
	var row platformOpenCodeKeyRow
	if r == nil || r.deps == nil || r.DB == nil {
		return row, false
	}
	result := r.DB.Where("id = ?", platformOpenCodeKeyID).Limit(1).Find(&row)
	return row, result.Error == nil && result.RowsAffected > 0
}

// SharedOpenCodeKey returns the key users without their own borrow.
func (r *UsersRepo) SharedOpenCodeKey() (SharedOpenCodeKey, bool) {
	row, ok := r.platformOpenCodeKey()
	if !ok || strings.TrimSpace(row.APIKey) == "" {
		return SharedOpenCodeKey{}, false
	}
	models := decodeSharedModels(row.Models)
	if len(models) == 0 {
		return SharedOpenCodeKey{}, false
	}
	return SharedOpenCodeKey{
		APIKey:   row.APIKey,
		BaseURL:  strings.TrimSpace(row.BaseURL),
		Protocol: row.Protocol,
		Models:   models,
	}, true
}

// SharedOpenCodeModels returns the models users without their own key may
// use, the default first, or nil when there is no shared key.
func (r *UsersRepo) SharedOpenCodeModels() []string {
	if key, ok := r.SharedOpenCodeKey(); ok {
		return key.Models
	}
	return nil
}

// PlatformOpenCodeKeyStatus describes the shared key for administrators.
func (r *UsersRepo) PlatformOpenCodeKeyStatus() PlatformOpenCodeKeyStatus {
	status := PlatformOpenCodeKeyStatus{Models: []string{}, Protocol: OpenCodeProtocolChat}
	if row, ok := r.platformOpenCodeKey(); ok && strings.TrimSpace(row.APIKey) != "" {
		status.Configured = true
		status.BaseURL = strings.TrimSpace(row.BaseURL)
		status.Protocol = row.Protocol
		if status.Protocol == "" {
			status.Protocol = OpenCodeProtocolChat
		}
		if models := decodeSharedModels(row.Models); models != nil {
			status.Models = models
		}
		status.UpdatedAt = row.Updated
		if user, ok := r.User(row.UpdatedBy); ok {
			status.UpdatedBy = user.LoginName
		}
	}
	return status
}

// SetPlatformOpenCodeKey saves the shared key settings.
func (r *UsersRepo) SetPlatformOpenCodeKey(update PlatformOpenCodeKeyUpdate, actorID int64) error {
	if len(update.Models) == 0 {
		return ErrSharedOpenCodeModelsRequired
	}
	row, _ := r.platformOpenCodeKey()
	if key := strings.TrimSpace(update.APIKey); key != "" {
		row.APIKey = key
	}
	if strings.TrimSpace(row.APIKey) == "" {
		return ErrSharedOpenCodeKeyRequired
	}
	models, err := json.Marshal(update.Models)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	row.ID = platformOpenCodeKeyID
	row.BaseURL = strings.TrimSpace(update.BaseURL)
	row.Protocol = strings.ToLower(strings.TrimSpace(update.Protocol))
	if row.Protocol == "" {
		row.Protocol = OpenCodeProtocolChat
	}
	row.Models = string(models)
	row.UpdatedBy = actorID
	row.Updated = &now
	return r.DB.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "id"}},
		DoUpdates: clause.AssignmentColumns([]string{"api_key", "base_url", "protocol", "models", "updated_by", "updated_at"}),
	}).Create(&row).Error
}

// ClearPlatformOpenCodeKey removes the shared key.
func (r *UsersRepo) ClearPlatformOpenCodeKey() error {
	return r.DB.Where("id = ?", platformOpenCodeKeyID).Delete(&platformOpenCodeKeyRow{}).Error
}
