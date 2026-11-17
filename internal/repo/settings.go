package repo

import (
	"strings"
	"time"

	"gorm.io/gorm/clause"
)

// SettingsRepo owns small platform-wide key/value settings that are not
// deployment configuration. It is deliberately separate from UsersRepo so a
// new setting does not turn into a user-domain method.
type SettingsRepo struct {
	*deps
}

const (
	SettingCodexSubscriptionEnabled         = "codex_subscription_enabled"
	SettingCodexSubscriptionAuthJSONEnabled = "codex_subscription_auth_json_enabled"
)

// PlatformSetting returns the stored value, or "" when the key is absent.
func (r *SettingsRepo) PlatformSetting(name string) string {
	if r == nil || r.deps == nil || r.DB == nil {
		return ""
	}
	var row platformSettingRow
	if err := r.DB.Where("name = ?", strings.TrimSpace(name)).Limit(1).Find(&row).Error; err != nil {
		return ""
	}
	return row.Value
}

// SetPlatformSetting upserts one setting. Empty values are stored as-is so a
// caller can explicitly clear a value.
func (r *SettingsRepo) SetPlatformSetting(name, value string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	now := time.Now().UTC()
	row := platformSettingRow{Name: name, Value: value, Updated: &now}
	return r.DB.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "name"}},
		DoUpdates: clause.AssignmentColumns([]string{"value", "updated_at"}),
	}).Create(&row).Error
}

func settingBool(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "on", "yes":
		return true
	default:
		return false
	}
}

func (r *SettingsRepo) CodexSubscriptionEnabled() bool {
	return settingBool(r.PlatformSetting(SettingCodexSubscriptionEnabled))
}

func (r *SettingsRepo) SetCodexSubscriptionEnabled(enabled bool) error {
	value := "0"
	if enabled {
		value = "1"
	}
	return r.SetPlatformSetting(SettingCodexSubscriptionEnabled, value)
}

func (r *SettingsRepo) CodexSubscriptionAuthJSONEnabled() bool {
	return settingBool(r.PlatformSetting(SettingCodexSubscriptionAuthJSONEnabled))
}

func (r *SettingsRepo) SetCodexSubscriptionAuthJSONEnabled(enabled bool) error {
	value := "0"
	if enabled {
		value = "1"
	}
	return r.SetPlatformSetting(SettingCodexSubscriptionAuthJSONEnabled, value)
}
