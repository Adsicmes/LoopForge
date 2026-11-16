package repo

import (
	"context"
	"log"
	"strings"
	"sync"
	"time"

	"agentflow/internal/domain"
	"agentflow/internal/gongfeng"
)

const gongfengRefreshLead = 10 * time.Minute

// gongfengRefreshTimeout bounds one refresh call, including waiting for
// Gongfeng to answer; a variable so tests can shorten it.
var gongfengRefreshTimeout = 20 * time.Second

type gongfengRefreshGate struct {
	mu        sync.Mutex
	failUntil time.Time
}

var gongfengRefreshGates sync.Map

// GitCredential returns the user's repository token, refreshing a Gongfeng
// OAuth grant shortly before it expires. A pasted personal access token has
// no refresh token and is returned unchanged. Refresh failures keep the
// current access token and back off for a minute.
func (r *UsersRepo) GitCredential(ctx context.Context, id int64) string {
	return r.GitCredentialFor(ctx, id, "")
}

// GitCredentialFor is GitCredential for one hosting platform, so a repository
// is cloned and pushed with the token that belongs to its platform.
func (r *UsersRepo) GitCredentialFor(ctx context.Context, id int64, provider string) string {
	user, ok := r.User(id)
	if !ok {
		return ""
	}
	platform := domain.NormalizeGitProvider(provider)
	if platform == "" {
		// The default slot follows the legacy behaviour: a Gongfeng grant (or a
		// stored Gongfeng token) is what git credentials resolve to, otherwise
		// GitHub.
		if strings.TrimSpace(user.GitTokens[domain.GitProviderGongfeng]) != "" || strings.TrimSpace(user.GongfengRefreshToken) != "" {
			platform = domain.GitProviderGongfeng
		} else {
			platform = domain.GitProviderGitHub
		}
	}
	if !strings.EqualFold(platform, domain.GitProviderGongfeng) {
		// Only Gongfeng OAuth grants expire; other platforms are plain tokens.
		return formatUserGitCredential(user, platform)
	}
	if !gongfengTokenDue(user, time.Now()) {
		return formatUserGitCredential(user, platform)
	}
	gate := gongfengRefreshGateFor(id)
	gate.mu.Lock()
	defer gate.mu.Unlock()
	if time.Now().Before(gate.failUntil) {
		user, _ = r.User(id)
		return formatUserGitCredential(user, platform)
	}
	user, ok = r.User(id)
	if !ok || !gongfengTokenDue(user, time.Now()) {
		if !ok {
			return ""
		}
		return formatUserGitCredential(user, platform)
	}
	cfg := gongfeng.ConfigFromEnv()
	if !cfg.Enabled() {
		return formatUserGitCredential(user, platform)
	}
	// Other callers for this user wait on the gate, and some pass
	// context.Background(), so the refresh carries its own deadline.
	refreshCtx, cancel := context.WithTimeout(ctx, gongfengRefreshTimeout)
	defer cancel()
	tok, err := gongfeng.Refresh(refreshCtx, cfg, user.GongfengRefreshToken)
	if err != nil {
		gate.failUntil = time.Now().Add(time.Minute)
		log.Printf("gongfeng oauth refresh for user %d failed: %v", id, err)
		return formatUserGitCredential(user, platform)
	}
	updated, err := r.SaveGongfengOAuth(id, tok.AccessToken, tok.RefreshToken, tok.Expiry(time.Now()))
	if err != nil {
		log.Printf("gongfeng oauth refresh for user %d could not be saved: %v", id, err)
		return "oauth2:" + strings.TrimSpace(tok.AccessToken)
	}
	return formatUserGitCredential(updated, domain.GitProviderGongfeng)
}

func formatUserGitCredential(user domain.User, provider string) string {
	tok := strings.TrimSpace(user.GitTokenFor(provider))
	if tok == "" {
		return ""
	}
	if strings.TrimSpace(user.GongfengRefreshToken) != "" && !strings.HasPrefix(strings.ToLower(tok), "oauth2:") {
		return "oauth2:" + tok
	}
	return tok
}

func gongfengTokenDue(user domain.User, now time.Time) bool {
	if strings.TrimSpace(user.GongfengRefreshToken) == "" {
		return false
	}
	if user.GongfengTokenExpiresAt.IsZero() {
		return true
	}
	return !user.GongfengTokenExpiresAt.After(now.Add(gongfengRefreshLead))
}

func gongfengRefreshGateFor(id int64) *gongfengRefreshGate {
	gate, _ := gongfengRefreshGates.LoadOrStore(id, &gongfengRefreshGate{})
	return gate.(*gongfengRefreshGate)
}
