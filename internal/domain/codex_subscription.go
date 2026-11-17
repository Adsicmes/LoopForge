package domain

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Codex authentication modes stored in user_configs.codex_auth_mode.
const (
	CodexAuthAPIKey       = "api_key"
	CodexAuthSubscription = "subscription"
	CodexAuthAccessToken  = "access_token"
	// CodexAuthGateway is runtime-only: it selects the platform gateway for a
	// runner invocation and is never stored in user_configs.
	CodexAuthGateway = "gateway"
)

var ErrCodexAuthInvalid = errors.New("invalid Codex auth.json")

// CodexAuthFile is the subset of Codex auth.json this platform reads. The
// original bytes are always preserved; this type is only used for validation
// and metadata.
type CodexAuthFile struct {
	AuthMode    *string          `json:"auth_mode,omitempty"`
	OpenAIKey   *string          `json:"OPENAI_API_KEY,omitempty"`
	Tokens      *CodexAuthTokens `json:"tokens,omitempty"`
	LastRefresh *time.Time       `json:"last_refresh,omitempty"`
}

type CodexAuthTokens struct {
	// IDToken is an object in Codex 0.162.1. Older files may store the raw JWT
	// as a string; both forms are accepted.
	IDToken      json.RawMessage `json:"id_token"`
	AccessToken  string          `json:"access_token"`
	RefreshToken string          `json:"refresh_token"`
	AccountID    *string         `json:"account_id,omitempty"`
}

// CodexIDToken is the flat claim shape Codex writes into auth.json.
type CodexIDToken struct {
	Email            string `json:"email,omitempty"`
	PlanType         string `json:"chatgpt_plan_type,omitempty"`
	ChatGPTUserID    string `json:"chatgpt_user_id,omitempty"`
	ChatGPTAccountID string `json:"chatgpt_account_id,omitempty"`
	RawJWT           string `json:"raw_jwt,omitempty"`
}

// CodexAuthMetadata is the non-secret metadata shown in personal settings.
type CodexAuthMetadata struct {
	Email     string
	PlanType  string
	AccountID string
}

// CodexSubscription is the decrypted credential record. It must never be
// serialized to an API response.
type CodexSubscription struct {
	UserID        int64
	AuthJSON      []byte
	Generation    int64
	AccountID     string
	Email         string
	PlanType      string
	ConnectedAt   time.Time
	LastRefreshAt time.Time
	NeedsReauth   bool
	LastError     string
}

// CodexSubscriptionStatus is the public, non-secret view for the owning user.
type CodexSubscriptionStatus struct {
	Configured    bool
	NeedsReauth   bool
	Email         string
	PlanType      string
	AccountID     string
	ConnectedAt   time.Time
	LastRefreshAt time.Time
	LastError     string
	Generation    int64
}

// ValidCodexAuthMode reports whether raw is one of the persisted modes.
// Empty means "not chosen yet".
func ValidCodexAuthMode(raw string) bool {
	switch strings.TrimSpace(raw) {
	case "", CodexAuthAPIKey, CodexAuthSubscription, CodexAuthAccessToken:
		return true
	default:
		return false
	}
}

// ParseCodexAuthJSON validates a Codex auth.json file. It requires a ChatGPT
// token bundle; API-key-only and enterprise access-token files are rejected
// with actionable messages.
func ParseCodexAuthJSON(raw []byte) (CodexAuthFile, error) {
	var file CodexAuthFile
	if len(raw) == 0 {
		return file, fmt.Errorf("%w: 文件为空", ErrCodexAuthInvalid)
	}
	if len(raw) > 64*1024 {
		return file, fmt.Errorf("%w: 文件超过 64 KB", ErrCodexAuthInvalid)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if err := decoder.Decode(&file); err != nil {
		return file, fmt.Errorf("%w: %v", ErrCodexAuthInvalid, err)
	}
	if file.AuthMode != nil {
		mode := strings.ToLower(strings.TrimSpace(*file.AuthMode))
		if mode != "" && mode != "chatgpt" {
			return file, fmt.Errorf("%w: auth_mode=%s；这是 API Key 或其他登录方式", ErrCodexAuthInvalid, mode)
		}
	}
	if file.Tokens == nil {
		if file.OpenAIKey != nil && strings.TrimSpace(*file.OpenAIKey) != "" {
			return file, fmt.Errorf("%w: 这是 API Key 登录，请改填 Codex API Key", ErrCodexAuthInvalid)
		}
		return file, fmt.Errorf("%w: 缺少 tokens 字段", ErrCodexAuthInvalid)
	}
	if strings.TrimSpace(file.Tokens.AccessToken) == "" || strings.TrimSpace(file.Tokens.RefreshToken) == "" {
		return file, fmt.Errorf("%w: 缺少 tokens.access_token 或 tokens.refresh_token", ErrCodexAuthInvalid)
	}
	if _, err := file.IDToken(); err != nil {
		return file, err
	}
	return file, nil
}

// IDToken returns the flat claim object, tolerating older raw-JWT storage.
func (f CodexAuthFile) IDToken() (CodexIDToken, error) {
	var out CodexIDToken
	if f.Tokens == nil || len(f.Tokens.IDToken) == 0 || string(f.Tokens.IDToken) == "null" {
		return out, fmt.Errorf("%w: 缺少 tokens.id_token", ErrCodexAuthInvalid)
	}
	raw := bytes.TrimSpace(f.Tokens.IDToken)
	if len(raw) > 0 && raw[0] == '"' {
		var jwt string
		if err := json.Unmarshal(raw, &jwt); err != nil || strings.TrimSpace(jwt) == "" {
			return out, fmt.Errorf("%w: tokens.id_token 字符串无效", ErrCodexAuthInvalid)
		}
		claims, err := parseCodexJWT(jwt)
		if err != nil {
			return out, err
		}
		claims.RawJWT = jwt
		return claims, nil
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, fmt.Errorf("%w: tokens.id_token 不是对象", ErrCodexAuthInvalid)
	}
	if out.RawJWT == "" && out.Email == "" && out.ChatGPTAccountID == "" {
		// The object is accepted even without claims; the account id may live
		// on tokens.account_id. Do not fail a valid login for missing metadata.
		return out, nil
	}
	return out, nil
}

// ParseCodexIDTokenJWT parses a raw id_token JWT into the flat claim shape
// Codex stores in auth.json.
func ParseCodexIDTokenJWT(jwt string) (CodexIDToken, error) {
	return parseCodexJWT(jwt)
}

func parseCodexJWT(jwt string) (CodexIDToken, error) {
	parts := strings.Split(jwt, ".")
	if len(parts) != 3 || parts[1] == "" {
		return CodexIDToken{}, fmt.Errorf("%w: id_token 不是 JWT", ErrCodexAuthInvalid)
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return CodexIDToken{}, fmt.Errorf("%w: id_token 载荷无效", ErrCodexAuthInvalid)
	}
	var claims struct {
		Email   string `json:"email"`
		Profile *struct {
			Email string `json:"email"`
		} `json:"https://api.openai.com/profile"`
		Auth *struct {
			PlanType  string `json:"chatgpt_plan_type"`
			UserID    string `json:"chatgpt_user_id"`
			AccountID string `json:"chatgpt_account_id"`
		} `json:"https://api.openai.com/auth"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return CodexIDToken{}, fmt.Errorf("%w: id_token 声明无效", ErrCodexAuthInvalid)
	}
	out := CodexIDToken{Email: claims.Email, RawJWT: jwt}
	if out.Email == "" && claims.Profile != nil {
		out.Email = claims.Profile.Email
	}
	if claims.Auth != nil {
		out.PlanType = claims.Auth.PlanType
		out.ChatGPTUserID = claims.Auth.UserID
		out.ChatGPTAccountID = claims.Auth.AccountID
	}
	return out, nil
}

// Metadata returns the non-secret account fields for display.
func (f CodexAuthFile) Metadata() (CodexAuthMetadata, error) {
	id, err := f.IDToken()
	if err != nil {
		return CodexAuthMetadata{}, err
	}
	accountID := id.ChatGPTAccountID
	if f.Tokens != nil && f.Tokens.AccountID != nil && strings.TrimSpace(*f.Tokens.AccountID) != "" {
		accountID = strings.TrimSpace(*f.Tokens.AccountID)
	}
	return CodexAuthMetadata{Email: strings.TrimSpace(id.Email), PlanType: DisplayCodexPlan(id.PlanType), AccountID: accountID}, nil
}

// DisplayCodexPlan maps Codex's raw plan values to the product names users
// recognize. Unknown values are shown as-is.
func DisplayCodexPlan(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "":
		return ""
	case "plus":
		return "Plus"
	case "pro":
		return "Pro"
	case "prolite":
		return "Pro Lite"
	case "promax":
		return "Pro Max"
	case "team":
		return "Team"
	case "business", "self_serve_business_prolite", "self_serve_business_usage_based":
		return "Business"
	case "enterprise", "hc", "ent26", "enterprise_cbp_automation", "enterprise_cbp_usage_based":
		return "Enterprise"
	case "education", "edu":
		return "Edu"
	case "edu_plus":
		return "Edu Plus"
	case "edu_pro":
		return "Edu Pro"
	case "free":
		return "Free"
	case "go":
		return "Go"
	default:
		return strings.TrimSpace(raw)
	}
}

// CodexAuthNeedsReauth recognizes the permanent refresh failures Codex emits.
func CodexAuthNeedsReauth(text string) bool {
	lower := strings.ToLower(text)
	for _, needle := range []string{
		"refresh_token_expired",
		"refresh_token_reused",
		"refresh_token_invalidated",
		"refresh token has expired",
		"refresh token was already used",
		"refresh token was revoked",
		"please log out and sign in again",
		"please sign in again",
	} {
		if strings.Contains(lower, needle) {
			return true
		}
	}
	return false
}
