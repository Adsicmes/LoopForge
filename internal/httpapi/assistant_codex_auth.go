package httpapi

import (
	"os"
	"strings"
	"time"

	"agentflow/internal/codexauth"
	"agentflow/internal/domain"
	"agentflow/internal/runtime"
)

// codexCredential resolves one chat turn's Codex credential. remote means the
// command will run on an execution machine; subscription turns there use a
// run-scoped gateway token instead of the auth.json.
func (h *assistantHandlers) codexCredential(user domain.User, remote bool, scope string) *runtime.ProviderCredential {
	if h == nil || h.repo == nil || user.ID <= 0 {
		return nil
	}
	if !domain.CredentialKeyConfigured() && (strings.TrimSpace(user.CodexAuthMode) == domain.CodexAuthSubscription || (strings.TrimSpace(user.CodexAPIKey) == "" && user.CodexSubscriptionConfigured)) {
		return &runtime.ProviderCredential{AuthMode: domain.CodexAuthSubscription, AuthError: "服务端未配置 AGENTFLOW_CREDENTIAL_KEY，Codex 订阅不可用；请联系平台管理员或改用 API Key"}
	}
	enabled := h.repo.CodexSubscriptionEnabled()
	sub, hasSub := h.repo.CodexSubscription(user.ID)
	hasKey := strings.TrimSpace(user.CodexAPIKey) != ""
	subUsable := hasSub && !sub.NeedsReauth && enabled
	mode := strings.TrimSpace(user.CodexAuthMode)

	subscription := func() *runtime.ProviderCredential {
		switch {
		case !enabled:
			return &runtime.ProviderCredential{AuthMode: domain.CodexAuthSubscription, AuthError: "Codex 订阅能力未开启，请联系平台管理员"}
		case !hasSub:
			if user.CodexSubscriptionConfigured {
				return &runtime.ProviderCredential{AuthMode: domain.CodexAuthSubscription, AuthError: "Codex 订阅凭据无法解密，请确认 AGENTFLOW_CREDENTIAL_KEY 未变更，或在个人配置中重新连接"}
			}
			return &runtime.ProviderCredential{AuthMode: domain.CodexAuthSubscription, AuthError: "已选择 Codex 订阅，但还没有连接；请在个人配置中重新连接"}
		case sub.NeedsReauth:
			return &runtime.ProviderCredential{AuthMode: domain.CodexAuthSubscription, AuthError: "Codex 订阅登录已失效，请在个人配置中重新连接"}
		case remote:
			baseURL := codexauth.GatewayURL()
			if baseURL == "" {
				return &runtime.ProviderCredential{AuthMode: domain.CodexAuthGateway, AuthError: "Codex 网关地址未配置，请联系平台管理员"}
			}
			token, expires, err := codexauth.Issue(user.ID, "", scope, 2*time.Hour)
			if err != nil {
				return &runtime.ProviderCredential{AuthMode: domain.CodexAuthGateway, AuthError: err.Error()}
			}
			return &runtime.ProviderCredential{AuthMode: domain.CodexAuthGateway, GatewayURL: baseURL, GatewayToken: token, GatewayExpiresAt: expires}
		default:
			return &runtime.ProviderCredential{AuthMode: domain.CodexAuthSubscription, AuthJSON: sub.AuthJSON, AuthGeneration: sub.Generation}
		}
	}

	switch mode {
	case domain.CodexAuthSubscription:
		return subscription()
	case domain.CodexAuthAPIKey:
		if !hasKey {
			return &runtime.ProviderCredential{AuthMode: domain.CodexAuthAPIKey, AuthError: "已选择 Codex API Key，但个人配置中没有填写 Key"}
		}
		return &runtime.ProviderCredential{AuthMode: domain.CodexAuthAPIKey, APIKey: user.CodexAPIKey, BaseURL: user.CodexBaseURL}
	case domain.CodexAuthAccessToken:
		return &runtime.ProviderCredential{AuthMode: domain.CodexAuthAccessToken, AuthError: "Codex access token 模式尚未启用"}
	}
	if hasKey {
		return &runtime.ProviderCredential{AuthMode: domain.CodexAuthAPIKey, APIKey: user.CodexAPIKey, BaseURL: user.CodexBaseURL}
	}
	if subUsable {
		return subscription()
	}
	return nil
}

// captureCodexAuth persists a token refreshed by a chat turn. A failed read or
// a stale generation leaves the file in place for the next newest-wins import.
func (h *assistantHandlers) captureCodexAuth(userID, generation int64, path string) {
	if h == nil || h.repo == nil || generation <= 0 {
		return
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return
	}
	ok, err := h.repo.CaptureCodexSubscription(userID, generation, raw)
	if err != nil || !ok {
		return
	}
	_ = os.Remove(path)
}
