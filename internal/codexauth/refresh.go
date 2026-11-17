package codexauth

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"agentflow/internal/domain"
	"agentflow/internal/repo"
)

const (
	defaultTokenURL = "https://auth.openai.com/oauth/token"
	defaultClientID = "app_EMoamEEZ73f0CkXaXp7hrann"
)

var (
	ErrNoCredential = errors.New("Codex subscription is not connected")
	ErrRefreshBusy  = errors.New("Codex subscription is refreshing, retry shortly")
)

// ReauthError means the refresh token is permanently unusable.
type ReauthError struct{ Message string }

func (e *ReauthError) Error() string { return e.Message }

// AccessToken returns a valid OAuth access token for userID, refreshing it
// under a per-user lease when it is within five minutes of expiry. The
// returned auth.json is the merged, current token bundle.
func AccessToken(ctx context.Context, store *repo.Store, userID int64) ([]byte, string, string, error) {
	if store == nil || userID <= 0 {
		return nil, "", "", ErrNoCredential
	}
	sub, ok := store.CodexSubscription(userID)
	if !ok {
		return nil, "", "", ErrNoCredential
	}
	file, err := domain.ParseCodexAuthJSON(sub.AuthJSON)
	if err != nil {
		return nil, "", "", err
	}
	if token, ok := validAccessToken(file, 5*time.Minute); ok {
		meta, _ := file.Metadata()
		return sub.AuthJSON, token, meta.AccountID, nil
	}

	owner := randomHex(8)
	acquired, err := store.AcquireCodexRefreshLease(userID, owner, 30*time.Second)
	if err != nil {
		return nil, "", "", err
	}
	if !acquired {
		// Another request may already be refreshing. Wait for its result
		// rather than consuming the rotating token a second time.
		deadline := time.NewTimer(10 * time.Second)
		defer deadline.Stop()
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return nil, "", "", ctx.Err()
			case <-deadline.C:
				return nil, "", "", ErrRefreshBusy
			case <-ticker.C:
				sub, ok = store.CodexSubscription(userID)
				if !ok {
					return nil, "", "", ErrNoCredential
				}
				file, err = domain.ParseCodexAuthJSON(sub.AuthJSON)
				if err != nil {
					return nil, "", "", err
				}
				if token, ok := validAccessToken(file, 5*time.Minute); ok {
					meta, _ := file.Metadata()
					return sub.AuthJSON, token, meta.AccountID, nil
				}
			}
		}
	}
	defer func() { _ = store.ReleaseCodexRefreshLease(userID, owner) }()

	sub, ok = store.CodexSubscription(userID)
	if !ok {
		return nil, "", "", ErrNoCredential
	}
	file, err = domain.ParseCodexAuthJSON(sub.AuthJSON)
	if err != nil {
		return nil, "", "", err
	}
	if token, ok := validAccessToken(file, 5*time.Minute); ok {
		meta, _ := file.Metadata()
		return sub.AuthJSON, token, meta.AccountID, nil
	}

	merged, err := refresh(ctx, sub.AuthJSON)
	if err != nil {
		var reauth *ReauthError
		if errors.As(err, &reauth) {
			_ = store.MarkCodexSubscriptionReauth(userID, sub.Generation, reauth.Message)
		}
		return nil, "", "", err
	}
	captured, err := store.CaptureCodexSubscription(userID, sub.Generation, merged)
	if err != nil {
		return nil, "", "", err
	}
	if !captured {
		// Another writer committed a newer bundle. Prefer it when valid.
		if latest, ok := store.CodexSubscription(userID); ok {
			if latestFile, parseErr := domain.ParseCodexAuthJSON(latest.AuthJSON); parseErr == nil {
				if token, valid := validAccessToken(latestFile, 0); valid {
					meta, _ := latestFile.Metadata()
					return latest.AuthJSON, token, meta.AccountID, nil
				}
			}
		}
		return nil, "", "", ErrRefreshBusy
	}
	mergedFile, err := domain.ParseCodexAuthJSON(merged)
	if err != nil {
		return nil, "", "", err
	}
	token, ok := validAccessToken(mergedFile, 0)
	if !ok {
		return nil, "", "", errors.New("Codex token refresh returned no usable access token")
	}
	meta, _ := mergedFile.Metadata()
	return merged, token, meta.AccountID, nil
}

func validAccessToken(file domain.CodexAuthFile, leeway time.Duration) (string, bool) {
	if file.Tokens == nil || strings.TrimSpace(file.Tokens.AccessToken) == "" {
		return "", false
	}
	exp, ok := jwtExpiry(file.Tokens.AccessToken)
	if !ok || exp.Before(time.Now().UTC().Add(leeway)) {
		return "", false
	}
	return file.Tokens.AccessToken, true
}

func jwtExpiry(token string) (time.Time, bool) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[1] == "" {
		return time.Time{}, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return time.Time{}, false
	}
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if json.Unmarshal(payload, &claims) != nil || claims.Exp <= 0 {
		return time.Time{}, false
	}
	return time.Unix(claims.Exp, 0).UTC(), true
}

func refresh(ctx context.Context, authJSON []byte) ([]byte, error) {
	file, err := domain.ParseCodexAuthJSON(authJSON)
	if err != nil {
		return nil, err
	}
	if file.Tokens == nil || strings.TrimSpace(file.Tokens.RefreshToken) == "" {
		return nil, &ReauthError{Message: "Codex 订阅缺少 refresh token，请重新连接"}
	}
	endpoint := strings.TrimSpace(os.Getenv("AGENTFLOW_CODEX_TOKEN_URL"))
	if endpoint == "" {
		endpoint = defaultTokenURL
	}
	clientID := strings.TrimSpace(os.Getenv("AGENTFLOW_CODEX_CLIENT_ID"))
	if clientID == "" {
		clientID = defaultClientID
	}
	body, _ := json.Marshal(map[string]string{
		"client_id":     clientID,
		"grant_type":    "refresh_token",
		"refresh_token": file.Tokens.RefreshToken,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		lower := strings.ToLower(string(raw))
		if domain.CodexAuthNeedsReauth(lower) || strings.Contains(lower, "invalid_grant") || strings.Contains(lower, "invalid_token") {
			return nil, &ReauthError{Message: "Codex 订阅登录已失效，请在个人配置中重新连接"}
		}
		return nil, fmt.Errorf("Codex token refresh failed: HTTP %d", resp.StatusCode)
	}
	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		IDToken      string `json:"id_token"`
	}
	if err := json.Unmarshal(raw, &tokenResp); err != nil {
		return nil, fmt.Errorf("Codex token refresh returned an invalid response: %w", err)
	}
	if strings.TrimSpace(tokenResp.AccessToken) == "" {
		return nil, errors.New("Codex token refresh returned an empty access token")
	}
	return mergeAuthJSON(authJSON, tokenResp.AccessToken, tokenResp.RefreshToken, tokenResp.IDToken)
}

func mergeAuthJSON(existing []byte, accessToken, refreshToken, idToken string) ([]byte, error) {
	var doc map[string]any
	if err := json.Unmarshal(existing, &doc); err != nil {
		return nil, err
	}
	tokens, ok := doc["tokens"].(map[string]any)
	if !ok {
		return nil, errors.New("Codex auth.json has no tokens object")
	}
	tokens["access_token"] = accessToken
	if strings.TrimSpace(refreshToken) != "" {
		tokens["refresh_token"] = refreshToken
	}
	if strings.TrimSpace(idToken) != "" {
		if claims, err := domain.ParseCodexIDTokenJWT(idToken); err == nil {
			flat := map[string]any{
				"email":              claims.Email,
				"chatgpt_plan_type":  claims.PlanType,
				"chatgpt_user_id":    claims.ChatGPTUserID,
				"chatgpt_account_id": claims.ChatGPTAccountID,
				"raw_jwt":            idToken,
			}
			tokens["id_token"] = flat
			if existingID, _ := tokens["account_id"].(string); strings.TrimSpace(existingID) == "" && claims.ChatGPTAccountID != "" {
				tokens["account_id"] = claims.ChatGPTAccountID
			}
		}
	}
	doc["last_refresh"] = time.Now().UTC().Format(time.RFC3339Nano)
	return json.Marshal(doc)
}
