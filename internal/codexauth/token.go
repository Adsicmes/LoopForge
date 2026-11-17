// Package codexauth issues run-scoped gateway tokens and refreshes ChatGPT
// OAuth tokens on the control plane. The refresh token never leaves this
// process; runners only receive short-lived gateway tokens.
package codexauth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	GatewaySecretEnv = "AGENTFLOW_CODEX_GATEWAY_SECRET"
	GatewayURLEnv    = "AGENTFLOW_CODEX_GATEWAY_URL"
	PublicURLEnv     = "AGENTFLOW_PUBLIC_URL"
	APIURLEnv        = "AGENTFLOW_API_URL"
)

var ErrGatewayToken = errors.New("invalid Codex gateway token")

type Claims struct {
	Version   int    `json:"v"`
	UserID    int64  `json:"uid"`
	RunID     string `json:"rid,omitempty"`
	SessionID string `json:"sid,omitempty"`
	Expires   int64  `json:"exp"`
	Nonce     string `json:"nonce,omitempty"`
}

func secret() ([]byte, error) {
	value := strings.TrimSpace(os.Getenv(GatewaySecretEnv))
	if value == "" {
		value = strings.TrimSpace(os.Getenv("AGENTFLOW_SESSION_SECRET"))
	}
	if value == "" {
		return nil, fmt.Errorf("Codex 网关密钥未配置：请设置 %s 或 AGENTFLOW_SESSION_SECRET", GatewaySecretEnv)
	}
	return []byte(value), nil
}

// GatewayURL returns the public gateway base URL Codex should use. Codex
// appends /responses to it.
func GatewayURL() string {
	if value := strings.TrimSpace(os.Getenv(GatewayURLEnv)); value != "" {
		return strings.TrimRight(value, "/")
	}
	base := strings.TrimSpace(os.Getenv(PublicURLEnv))
	if base == "" {
		base = strings.TrimSpace(os.Getenv(APIURLEnv))
	}
	if base == "" {
		return ""
	}
	parsed, err := url.Parse(base)
	if err != nil || parsed.Host == "" {
		return ""
	}
	return strings.TrimRight(base, "/") + "/api/codex-gateway"
}

// Issue creates a signed run-scoped gateway token. The token is only valid at
// this platform's gateway and cannot be used against OpenAI directly.
func Issue(userID int64, runID, sessionID string, ttl time.Duration) (string, time.Time, error) {
	if userID <= 0 {
		return "", time.Time{}, errors.New("gateway token requires a user")
	}
	key, err := secret()
	if err != nil {
		return "", time.Time{}, err
	}
	if ttl <= 0 {
		ttl = 2 * time.Hour
	}
	expires := time.Now().UTC().Add(ttl)
	claims := Claims{
		Version:   1,
		UserID:    userID,
		RunID:     strings.TrimSpace(runID),
		SessionID: strings.TrimSpace(sessionID),
		Expires:   expires.Unix(),
		Nonce:     randomHex(12),
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", time.Time{}, err
	}
	body := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(body))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return body + "." + sig, expires, nil
}

// Verify validates a gateway token and returns its claims.
func Verify(token string) (Claims, error) {
	key, err := secret()
	if err != nil {
		return Claims{}, err
	}
	parts := strings.Split(strings.TrimSpace(token), ".")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return Claims{}, ErrGatewayToken
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(parts[0]))
	expected, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || !hmac.Equal(expected, mac.Sum(nil)) {
		return Claims{}, ErrGatewayToken
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return Claims{}, ErrGatewayToken
	}
	var claims Claims
	if err := json.Unmarshal(payload, &claims); err != nil || claims.UserID <= 0 || claims.Expires <= 0 {
		return Claims{}, ErrGatewayToken
	}
	if time.Now().UTC().Unix() >= claims.Expires {
		return Claims{}, fmt.Errorf("%w: expired", ErrGatewayToken)
	}
	return claims, nil
}

func randomHex(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("%x", buf)
}
