package domain

import (
	"strings"
	"time"
)

// User 表示系统注册或登录的员工实体，用于后续资源归属与权限隔离。
type User struct {
	CodexAPIKey                string `json:"codexApiKey,omitempty"`
	CodexAPIKeyConfigured      bool   `json:"codexApiKeyConfigured,omitempty"`
	CodexBaseURL               string `json:"codexBaseUrl,omitempty"`
	ClaudeCodeAPIKey           string `json:"claudeCodeApiKey,omitempty"`
	ClaudeCodeAPIKeyConfigured bool   `json:"claudeCodeApiKeyConfigured,omitempty"`
	ClaudeCodeBaseURL          string `json:"claudeCodeBaseUrl,omitempty"`
	// CodexModels and ClaudeCodeModels are the model catalogs AI 对话 offers
	// for that CLI, in display order. Empty follows the CLI's built-in models
	// on the official endpoint, or the models the Base URL publishes.
	CodexModels      []CLIModel `json:"codexModels,omitempty"`
	ClaudeCodeModels []CLIModel `json:"claudeCodeModels,omitempty"`
	// AgentCLICredentials holds the credentials of the agent CLIs
	// (AgentCLIExecutorKinds) by kind.
	AgentCLICredentials map[string]AgentCLICredential `json:"agentCliCredentials,omitempty"`
	ID                  int64                         `json:"id"`
	StaffID             int                           `json:"staffId"`
	LoginName           string                        `json:"loginName"`
	ChineseName         string                        `json:"chineseName"`
	DeptID              int                           `json:"deptId"`
	DeptName            string                        `json:"deptName"`
	// PlatformRole is a platform-wide role, independent of project membership.
	// "admin" may edit generic SOP templates; empty is a regular user.
	PlatformRole string `json:"platformRole,omitempty"`
	// GitTokens holds one repository token per hosting platform (github,
	// gongfeng, codeup). GitToken is the pre-multi-provider single slot: reads
	// fall back to it and writes mirror the github entry into it, so an older
	// client or an untouched row keeps working.
	GitTokens          map[string]string `json:"-"`
	GitToken           string            `json:"gitToken,omitempty"`
	GitTokenConfigured bool              `json:"gitTokenConfigured,omitempty"`
	GitTokenOAuth      bool              `json:"gitTokenOAuth,omitempty"`
	// GitTokenProviders lists the platforms that hold a token, so the settings
	// panel can show one row per platform.
	GitTokenProviders            []string   `json:"gitTokenProviders,omitempty"`
	GitSSHKeyConfigured          bool       `json:"gitSshKeyConfigured,omitempty"`
	GongfengRefreshToken         string     `json:"-"`
	GongfengTokenExpiresAt       time.Time  `json:"-"`
	CodeBuddyAPIKey              string     `json:"codeBuddyApiKey,omitempty"`
	CodeBuddyAPIKeyConfigured    bool       `json:"codeBuddyApiKeyConfigured,omitempty"`
	CodeBuddyInternetEnvironment string     `json:"codeBuddyInternetEnvironment,omitempty"`
	CodeBuddyModels              []CLIModel `json:"codeBuddyModels,omitempty"`
	CreatedAt                    time.Time  `json:"createdAt"`
	UpdatedAt                    time.Time  `json:"updatedAt"`
}

const PlatformRoleAdmin = "admin"

// GitSSHKey is the public description of a personal SSH credential.
type GitSSHKey struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Host string `json:"host"`
}

// NormalizeCodeBuddyInternetEnvironment maps a network environment chosen in
// personal settings to the CODEBUDDY_INTERNET_ENVIRONMENT value: "" for the
// international edition, "internal" for the China edition and "ioa" for iOA.
// The CLI silently falls back to the international edition for any value it
// does not recognize, so unknown values report ok=false instead of being saved.
func NormalizeCodeBuddyInternetEnvironment(raw string) (value string, ok bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "public", "intl", "international", "国际站":
		return "", true
	case "internal", "china", "cn", "国内站":
		return "internal", true
	case "ioa":
		return "ioa", true
	}
	return "", false
}

// Git hosting platforms. A user may hold one personal access token per
// platform at the same time; each repository is cloned and pushed with the
// token of the platform it lives on.
const (
	GitProviderGitHub   = "github"
	GitProviderGongfeng = "gongfeng"
	GitProviderCodeup   = "codeup"
)

// NormalizeGitProvider maps a platform name to its canonical key. Unknown
// values report "" so callers can reject them instead of guessing.
func NormalizeGitProvider(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case GitProviderGitHub, "github.com", "gh":
		return GitProviderGitHub
	case GitProviderGongfeng, "git.code.tencent.com", "工蜂":
		return GitProviderGongfeng
	case GitProviderCodeup, "codeup.aliyun.com", "yunxiao", "云效":
		return GitProviderCodeup
	}
	return ""
}

// GitTokenFor returns the token stored for a platform. Rows written before
// tokens were kept per platform keep their single slot: its platform is
// inferred from a GitHub token prefix or from a Gongfeng OAuth grant, and an
// unrecognisable token is returned for any platform so the old behaviour
// survives until the row is written again.
func (u User) GitTokenFor(provider string) string {
	platform := NormalizeGitProvider(provider)
	if platform == "" {
		platform = GitProviderGitHub
	}
	if token := strings.TrimSpace(u.GitTokens[platform]); token != "" {
		return token
	}
	legacy := strings.TrimSpace(u.GitToken)
	if legacy == "" {
		return ""
	}
	if len(u.GitTokens) > 0 {
		// The row already stores per-platform tokens, so the legacy value is
		// not a stand-in for a platform that deliberately has none.
		return ""
	}
	if inferred := legacyGitProvider(u, legacy); inferred != "" && inferred != platform {
		return ""
	}
	return legacy
}

// legacyGitProvider infers which platform a pre-migration single token
// belongs to.
func legacyGitProvider(u User, token string) string {
	if strings.TrimSpace(u.GongfengRefreshToken) != "" {
		return GitProviderGongfeng
	}
	lower := strings.ToLower(strings.TrimPrefix(token, "oauth2:"))
	for _, prefix := range []string{"ghp_", "gho_", "ghu_", "ghs_", "ghr_", "github_pat_"} {
		if strings.HasPrefix(lower, prefix) {
			return GitProviderGitHub
		}
	}
	return ""
}

// GitTokenProvidersOf lists the platforms holding a token, in display order.
func (u User) GitTokenProvidersOf() []string {
	providers := make([]string, 0, 3)
	for _, platform := range []string{GitProviderGitHub, GitProviderGongfeng, GitProviderCodeup} {
		if u.GitTokenFor(platform) != "" {
			providers = append(providers, platform)
		}
	}
	return providers
}

// SetGitToken stores a token for one platform and drops the OAuth grant when
// a pasted token replaces it.
func (u *User) SetGitToken(provider, token string) {
	platform := NormalizeGitProvider(provider)
	if platform == "" {
		platform = GitProviderGitHub
	}
	if u.GitTokens == nil {
		u.GitTokens = map[string]string{}
	}
	u.GitTokens[platform] = token
	if platform == GitProviderGongfeng {
		u.GongfengRefreshToken = ""
		u.GongfengTokenExpiresAt = time.Time{}
	}
	u.syncLegacyGitToken()
}

// syncLegacyGitToken keeps the pre-multi-platform column pointing at the
// token an older reader would have found: 工蜂 when one is stored (its OAuth
// grant used to own that slot), otherwise GitHub.
func (u *User) syncLegacyGitToken() {
	if token := strings.TrimSpace(u.GitTokens[GitProviderGongfeng]); token != "" {
		u.GitToken = token
		return
	}
	u.GitToken = strings.TrimSpace(u.GitTokens[GitProviderGitHub])
}

// ClearGitToken drops the token of one platform and leaves the others alone.
func (u *User) ClearGitToken(provider string) {
	platform := NormalizeGitProvider(provider)
	if platform == "" {
		platform = GitProviderGitHub
	}
	delete(u.GitTokens, platform)
	if platform == GitProviderGongfeng || len(u.GitTokens) == 0 {
		u.GongfengRefreshToken = ""
		u.GongfengTokenExpiresAt = time.Time{}
	}
	u.syncLegacyGitToken()
}

// GitProviderForRepository reports which platform a repository lives on, so
// the run uses that platform's token.
func GitProviderForRepository(r Repository) string {
	return gitProviderFor(r.Provider, r.CloneURL, r.WebURL)
}

// GitProviderForRepositoryContext is GitProviderForRepository for a run's
// repository snapshot.
func GitProviderForRepositoryContext(r RepositoryContext) string {
	return gitProviderFor(r.Provider, r.CloneURL, r.WebURL)
}

func gitProviderFor(provider, cloneURL, webURL string) string {
	if platform := NormalizeGitProvider(provider); platform != "" {
		return platform
	}
	for _, raw := range []string{cloneURL, webURL} {
		lower := strings.ToLower(strings.TrimSpace(raw))
		switch {
		case strings.HasPrefix(lower, "codeup:"), strings.Contains(lower, "codeup.aliyun.com"):
			return GitProviderCodeup
		case strings.Contains(lower, "git.code.tencent.com"):
			return GitProviderGongfeng
		case strings.Contains(lower, "github.com"):
			return GitProviderGitHub
		}
	}
	return ""
}

// IsPlatformAdmin reports whether the user holds the platform-admin role.
func IsPlatformAdmin(u User) bool {
	return strings.EqualFold(strings.TrimSpace(u.PlatformRole), PlatformRoleAdmin)
}

// PublicUser returns user directory information without exposing credentials.
func PublicUser(u User) User {
	u.CodexAPIKeyConfigured = strings.TrimSpace(u.CodexAPIKey) != ""
	u.CodexAPIKey = ""
	u.ClaudeCodeAPIKeyConfigured = strings.TrimSpace(u.ClaudeCodeAPIKey) != ""
	u.ClaudeCodeAPIKey = ""
	u.GitTokenProviders = u.GitTokenProvidersOf()
	u.GitTokenConfigured = len(u.GitTokenProviders) > 0
	u.GitTokenOAuth = strings.TrimSpace(u.GongfengRefreshToken) != ""
	u.GitToken = ""
	u.GitTokens = nil
	u.GongfengRefreshToken = ""
	u.GongfengTokenExpiresAt = time.Time{}
	u.CodeBuddyAPIKeyConfigured = strings.TrimSpace(u.CodeBuddyAPIKey) != ""
	u.CodeBuddyAPIKey = ""
	if len(u.AgentCLICredentials) > 0 {
		public := make(map[string]AgentCLICredential, len(u.AgentCLICredentials))
		for kind, cred := range u.AgentCLICredentials {
			cred.APIKeyConfigured = strings.TrimSpace(cred.APIKey) != ""
			cred.APIKey = ""
			public[kind] = cred
		}
		u.AgentCLICredentials = public
	}
	return u
}

// CLIModel is one entry of a CLI model catalog. ID is passed to the CLI
// as-is; an empty Name shows the ID.
//
// Efforts is set on a Cursor model family folded out of the CLI's own model
// list: it maps each reasoning effort onto the model ID cursor-agent printed
// for it, and DefaultEffort is the effort an empty choice runs. They are
// saved with the catalog so the family resolves for its owner on any
// instance and after a restart.
type CLIModel struct {
	ID            string            `json:"id"`
	Name          string            `json:"name,omitempty"`
	Efforts       map[string]string `json:"efforts,omitempty"`
	DefaultEffort string            `json:"defaultEffort,omitempty"`
}

// AgentCLICredential is the personal configuration of one agent CLI. An
// empty BaseURL means the CLI's official endpoint; Protocol is the wire
// protocol BaseURL speaks (AgentCLIProtocolOptions).
type AgentCLICredential struct {
	APIKey           string     `json:"apiKey,omitempty"`
	APIKeyConfigured bool       `json:"apiKeyConfigured,omitempty"`
	BaseURL          string     `json:"baseUrl,omitempty"`
	Protocol         string     `json:"protocol,omitempty"`
	Models           []CLIModel `json:"models,omitempty"`
	ModelsCustomized bool       `json:"modelsCustomized,omitempty"` // Cursor: preserve explicit edits across discovery.
}

// Wire protocols an agent CLI can speak to a user Base URL.
const (
	AgentCLIProtocolChat      = "chat"      // OpenAI Chat Completions
	AgentCLIProtocolResponses = "responses" // OpenAI Responses
	AgentCLIProtocolMessages  = "messages"  // Anthropic Messages
	AgentCLIProtocolGemini    = "gemini"    // Google Gemini API
)

// AgentCLIProtocolOptions lists the protocols each agent CLI speaks against a
// user Base URL; the first one is the default. Cursor only talks to Cursor.
var AgentCLIProtocolOptions = map[string][]string{
	"opencode": {AgentCLIProtocolChat, AgentCLIProtocolResponses, AgentCLIProtocolMessages},
	"kimi":     {AgentCLIProtocolChat, AgentCLIProtocolResponses, AgentCLIProtocolMessages},
	"pi":       {AgentCLIProtocolChat, AgentCLIProtocolResponses, AgentCLIProtocolMessages},
	"dsh":      {AgentCLIProtocolChat, AgentCLIProtocolResponses, AgentCLIProtocolMessages},
	"zcode":    {AgentCLIProtocolMessages, AgentCLIProtocolChat, AgentCLIProtocolResponses},
	"agy":      {AgentCLIProtocolGemini, AgentCLIProtocolChat},
}

// NormalizeAgentCLIProtocol returns a supported protocol choice; unknown
// values fall back to the CLI's default.
func NormalizeAgentCLIProtocol(kind, protocol string) string {
	options := AgentCLIProtocolOptions[strings.TrimSpace(kind)]
	for _, option := range options {
		if option == strings.TrimSpace(protocol) {
			return option
		}
	}
	if len(options) == 0 {
		return ""
	}
	return options[0]
}

func PublicUsers(items []User) []User {
	out := make([]User, len(items))
	for i, u := range items {
		out[i] = PublicUser(u)
	}
	return out
}

// APIToken is a user-owned credential for REST API and MCP access.
// TokenHash is persisted but never serialized; the plaintext token is shown
// only once when it is created.
type APIToken struct {
	ID         string     `json:"id"`
	UserID     int64      `json:"userId"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"`
	TokenHash  string     `json:"-"`
	LastUsedAt *time.Time `json:"lastUsedAt,omitempty"`
	ExpiresAt  *time.Time `json:"expiresAt,omitempty"`
	CreatedAt  time.Time  `json:"createdAt"`
	RevokedAt  *time.Time `json:"revokedAt,omitempty"`
}
