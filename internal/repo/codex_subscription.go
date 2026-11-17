package repo

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"agentflow/internal/domain"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// CodexSubscription returns the decrypted personal ChatGPT credential. It is
// only called by the control plane; callers must never serialize it.
func (r *UsersRepo) CodexSubscription(userID int64) (domain.CodexSubscription, bool) {
	if r == nil || r.deps == nil || r.DB == nil || userID <= 0 {
		return domain.CodexSubscription{}, false
	}
	var row userCodexSubscriptionRow
	if err := r.DB.Where("user_id = ?", userID).Limit(1).Find(&row).Error; err != nil || row.UserID == 0 {
		return domain.CodexSubscription{}, false
	}
	authJSON, err := domain.OpenCredential([]byte(row.AuthJSONCiphertext))
	if err != nil {
		return domain.CodexSubscription{}, false
	}
	out := domain.CodexSubscription{
		UserID:      row.UserID,
		AuthJSON:    authJSON,
		Generation:  row.Generation,
		AccountID:   row.AccountID,
		Email:       row.Email,
		PlanType:    row.PlanType,
		ConnectedAt: row.ConnectedAt,
		NeedsReauth: row.NeedsReauth,
		LastError:   row.LastError,
	}
	if row.LastRefreshAt != nil {
		out.LastRefreshAt = *row.LastRefreshAt
	}
	return out, true
}

// CodexSubscriptionStatus returns the public status without decrypting the
// credential. It is safe for the owner's settings endpoint.
func (r *UsersRepo) CodexSubscriptionStatus(userID int64) (domain.CodexSubscriptionStatus, bool) {
	if r == nil || r.deps == nil || r.DB == nil || userID <= 0 {
		return domain.CodexSubscriptionStatus{}, false
	}
	var row userCodexSubscriptionRow
	if err := r.DB.Where("user_id = ?", userID).Limit(1).Find(&row).Error; err != nil || row.UserID == 0 {
		return domain.CodexSubscriptionStatus{}, false
	}
	out := domain.CodexSubscriptionStatus{
		Configured:  true,
		NeedsReauth: row.NeedsReauth,
		Email:       row.Email,
		PlanType:    row.PlanType,
		AccountID:   row.AccountID,
		ConnectedAt: row.ConnectedAt,
		LastError:   row.LastError,
		Generation:  row.Generation,
	}
	if _, err := domain.OpenCredential([]byte(row.AuthJSONCiphertext)); err != nil {
		out.LastError = "凭据无法解密：请确认 AGENTFLOW_CREDENTIAL_KEY 未变更，或重新连接订阅"
	}
	if row.LastRefreshAt != nil {
		out.LastRefreshAt = *row.LastRefreshAt
	}
	return out, true
}

// SaveCodexSubscriptionAndMode stores a newly connected/reconnected
// credential and selects subscription auth in one transaction. The
// generation increments so in-flight runs cannot write stale tokens back.
func (r *UsersRepo) SaveCodexSubscriptionAndMode(userID int64, authJSON []byte) (domain.CodexSubscription, error) {
	if userID <= 0 {
		return domain.CodexSubscription{}, errors.New("user not found")
	}
	file, err := domain.ParseCodexAuthJSON(authJSON)
	if err != nil {
		return domain.CodexSubscription{}, err
	}
	meta, err := file.Metadata()
	if err != nil {
		return domain.CodexSubscription{}, err
	}
	sealed, err := domain.SealCredential(authJSON)
	if err != nil {
		return domain.CodexSubscription{}, err
	}
	now := time.Now().UTC()
	var saved domain.CodexSubscription
	err = r.DB.Transaction(func(tx *gorm.DB) error {
		var existing userCodexSubscriptionRow
		_ = tx.Where("user_id = ?", userID).Limit(1).Find(&existing).Error
		generation := existing.Generation + 1
		if generation < 1 {
			generation = 1
		}
		connectedAt := now
		if !existing.ConnectedAt.IsZero() {
			connectedAt = existing.ConnectedAt
		}
		lastRefresh := now
		if file.LastRefresh != nil {
			lastRefresh = file.LastRefresh.UTC()
		}
		row := userCodexSubscriptionRow{
			UserID:             userID,
			AuthJSONCiphertext: string(sealed),
			Generation:         generation,
			AccountID:          meta.AccountID,
			Email:              meta.Email,
			PlanType:           meta.PlanType,
			ConnectedAt:        connectedAt,
			LastRefreshAt:      &lastRefresh,
			NeedsReauth:        false,
			LastError:          "",
			Updated:            &now,
		}
		if err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "user_id"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"auth_json_ciphertext", "generation", "account_id", "email", "plan_type",
				"connected_at", "last_refresh_at", "needs_reauth", "last_error",
				"run_lease_owner", "run_lease_until", "refresh_lease_owner", "refresh_lease_until", "updated_at",
			}),
		}).Create(&row).Error; err != nil {
			return err
		}
		// A new login invalidates any stale lease from the previous credential.
		if err := tx.Model(&userCodexSubscriptionRow{}).Where("user_id = ?", userID).Updates(map[string]any{
			"run_lease_owner": "", "run_lease_until": nil,
			"refresh_lease_owner": "", "refresh_lease_until": nil,
		}).Error; err != nil {
			return err
		}
		if err := upsertColumns(tx, "user_configs", map[string]any{
			"user_id":         userID,
			"codex_auth_mode": domain.CodexAuthSubscription,
		}, []string{"codex_auth_mode"}); err != nil {
			return err
		}
		saved = domain.CodexSubscription{
			UserID:        userID,
			AuthJSON:      authJSON,
			Generation:    generation,
			AccountID:     meta.AccountID,
			Email:         meta.Email,
			PlanType:      meta.PlanType,
			ConnectedAt:   connectedAt,
			LastRefreshAt: lastRefresh,
		}
		return nil
	})
	if err != nil {
		return domain.CodexSubscription{}, err
	}
	return saved, nil
}

// CaptureCodexSubscription writes tokens refreshed by a run or the gateway.
// It only succeeds while generation still matches and the incoming
// last_refresh is not older than the stored one.
func (r *UsersRepo) CaptureCodexSubscription(userID, generation int64, authJSON []byte) (bool, error) {
	if userID <= 0 || generation <= 0 {
		return false, nil
	}
	file, err := domain.ParseCodexAuthJSON(authJSON)
	if err != nil {
		return false, err
	}
	meta, err := file.Metadata()
	if err != nil {
		return false, err
	}
	now := time.Now().UTC()
	lastRefresh := now
	if file.LastRefresh != nil {
		lastRefresh = file.LastRefresh.UTC()
	}
	sealed, err := domain.SealCredential(authJSON)
	if err != nil {
		return false, err
	}
	result := r.DB.Model(&userCodexSubscriptionRow{}).
		Where("user_id = ? AND generation = ? AND (last_refresh_at IS NULL OR last_refresh_at <= ?)", userID, generation, lastRefresh).
		Updates(map[string]any{
			"auth_json_ciphertext": string(sealed),
			"generation":           gorm.Expr("generation + 1"),
			"account_id":           meta.AccountID,
			"email":                meta.Email,
			"plan_type":            meta.PlanType,
			"last_refresh_at":      lastRefresh,
			"needs_reauth":         false,
			"last_error":           "",
			"updated_at":           now,
		})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

// MarkCodexSubscriptionReauth flags a credential whose refresh token was
// permanently rejected. generation keeps a late failure from clobbering a
// newer reconnect.
func (r *UsersRepo) MarkCodexSubscriptionReauth(userID, generation int64, message string) error {
	if userID <= 0 {
		return nil
	}
	message = strings.TrimSpace(message)
	if len(message) > 4096 {
		message = message[:4096]
	}
	now := time.Now().UTC()
	q := r.DB.Model(&userCodexSubscriptionRow{}).Where("user_id = ?", userID)
	if generation > 0 {
		q = q.Where("generation = ?", generation)
	}
	return q.Updates(map[string]any{
		"needs_reauth": true,
		"last_error":   message,
		"updated_at":   now,
	}).Error
}

// DeleteCodexSubscriptionAndMode removes the credential and clears the
// explicit auth mode in one transaction.
func (r *UsersRepo) DeleteCodexSubscriptionAndMode(userID int64) error {
	if userID <= 0 {
		return nil
	}
	return r.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("user_id = ?", userID).Delete(&userCodexSubscriptionRow{}).Error; err != nil {
			return err
		}
		return tx.Model(&userConfigRow{}).Where("user_id = ?", userID).Update("codex_auth_mode", "").Error
	})
}

// CountCodexSubscriptions reports how many users have a connected credential.
// It never returns account identifiers.
func (r *UsersRepo) CountCodexSubscriptions() int64 {
	var count int64
	_ = r.DB.Model(&userCodexSubscriptionRow{}).Count(&count).Error
	return count
}

// AcquireCodexRunLease serializes V1 platform-hosted subscription runs per user.
// A run whose process dies is released by the lease deadline.
func (r *UsersRepo) AcquireCodexRunLease(userID int64, owner string, ttl time.Duration) (bool, error) {
	return r.acquireLease(userID, "run", owner, ttl)
}

func (r *UsersRepo) ReleaseCodexRunLease(userID int64, owner string) error {
	return r.releaseLease(userID, "run", owner)
}

// AcquireCodexRefreshLease serializes OAuth refresh in V2 so concurrent gateway
// requests cannot consume the same rotating refresh token.
func (r *UsersRepo) AcquireCodexRefreshLease(userID int64, owner string, ttl time.Duration) (bool, error) {
	return r.acquireLease(userID, "refresh", owner, ttl)
}

func (r *UsersRepo) ReleaseCodexRefreshLease(userID int64, owner string) error {
	return r.releaseLease(userID, "refresh", owner)
}

func (r *UsersRepo) acquireLease(userID int64, kind, owner string, ttl time.Duration) (bool, error) {
	if userID <= 0 || strings.TrimSpace(owner) == "" {
		return false, errors.New("lease owner is required")
	}
	if ttl <= 0 {
		ttl = 30 * time.Second
	}
	now := time.Now().UTC()
	until := now.Add(ttl)
	ownerCol, untilCol := kind+"_lease_owner", kind+"_lease_until"
	result := r.DB.Model(&userCodexSubscriptionRow{}).
		Where(fmt.Sprintf("user_id = ? AND (%s IS NULL OR %s < ? OR %s = ?)", untilCol, untilCol, ownerCol), userID, now, owner).
		Updates(map[string]any{ownerCol: owner, untilCol: until, "updated_at": now})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

func (r *UsersRepo) releaseLease(userID int64, kind, owner string) error {
	if userID <= 0 {
		return nil
	}
	ownerCol, untilCol := kind+"_lease_owner", kind+"_lease_until"
	return r.DB.Model(&userCodexSubscriptionRow{}).
		Where("user_id = ? AND "+ownerCol+" = ?", userID, owner).
		Updates(map[string]any{ownerCol: "", untilCol: nil, "updated_at": time.Now().UTC()}).Error
}
