package httpapi

import (
	"context"
	"strings"

	"agentflow/internal/domain"
	"agentflow/internal/repo"
	"agentflow/internal/runtime"
)

// assistantSharedOpenCode returns the platform credential users without their
// own OpenCode key borrow.
func (h *assistantHandlers) assistantSharedOpenCode() (repo.SharedOpenCodeKey, bool) {
	if h == nil || h.deps == nil || h.repo == nil {
		return repo.SharedOpenCodeKey{}, false
	}
	return h.repo.SharedOpenCodeKey()
}

// assistantOpenCodeUser fills a user's OpenCode credential from the platform
// shared record when the user has no personal key. The returned value is a
// copy; the stored user is never modified.
func (h *assistantHandlers) assistantOpenCodeUser(user domain.User) domain.User {
	if strings.TrimSpace(user.AgentCLICredentials[runtime.AgentCLIOpenCode].APIKey) != "" {
		return user
	}
	shared, ok := h.assistantSharedOpenCode()
	if !ok {
		return user
	}
	return withSharedOpenCode(user, shared)
}

// openCodeFirstModel resolves the default model of an OpenCode credential.
// A personal credential may not have a saved list; its gateway is asked once
// and the result is cached by the shared discovery cache.
func openCodeFirstModel(ctx context.Context, credential runtime.ProviderCredential) string {
	if len(credential.Models) > 0 {
		return credential.Models[0].ID
	}
	if strings.TrimSpace(credential.BaseURL) == "" {
		return ""
	}
	discovered := cachedAssistantEngineDiscovery(ctx, credential.Protocol == repo.OpenCodeProtocolMessages, credential.APIKey, credential.BaseURL)
	if len(discovered) > 0 {
		return discovered[0].ID
	}
	return ""
}

func withSharedOpenCode(user domain.User, shared repo.SharedOpenCodeKey) domain.User {
	if user.AgentCLICredentials == nil {
		user.AgentCLICredentials = map[string]domain.AgentCLICredential{}
	}
	cred := user.AgentCLICredentials[runtime.AgentCLIOpenCode]
	cred.APIKey = shared.APIKey
	cred.APIKeyConfigured = true
	cred.BaseURL = shared.BaseURL
	cred.Protocol = shared.ProtocolOrDefault()
	cred.Models = nil
	for _, id := range shared.Models {
		cred.Models = append(cred.Models, domain.CLIModel{ID: id, Name: id})
	}
	user.AgentCLICredentials[runtime.AgentCLIOpenCode] = cred
	return user
}

// openCodeProviderCredential resolves the personal or platform-shared OpenCode
// credential for a user. It is used by non-chat pipelines (clarification,
// reports, bots, evals) that do not go through the chat request path.
func openCodeProviderCredential(store *repo.Store, userID int64) (runtime.ProviderCredential, bool) {
	if store == nil || userID <= 0 {
		return runtime.ProviderCredential{}, false
	}
	if user, ok := store.User(userID); ok {
		cred := user.AgentCLICredentials[runtime.AgentCLIOpenCode]
		if strings.TrimSpace(cred.APIKey) != "" {
			return runtime.ProviderCredential{
				APIKey: cred.APIKey, BaseURL: cred.BaseURL, Protocol: cred.Protocol, Models: cred.Models,
			}, true
		}
	}
	shared, ok := store.SharedOpenCodeKey()
	if !ok {
		return runtime.ProviderCredential{}, false
	}
	credential := runtime.ProviderCredential{
		APIKey: shared.APIKey, BaseURL: shared.BaseURL, Protocol: shared.ProtocolOrDefault(), Sponsored: true,
	}
	for _, id := range shared.Models {
		credential.Models = append(credential.Models, domain.CLIModel{ID: id, Name: id})
	}
	return credential, true
}

func withOpenCodeCredential(user domain.User, credential runtime.ProviderCredential) domain.User {
	if user.AgentCLICredentials == nil {
		user.AgentCLICredentials = map[string]domain.AgentCLICredential{}
	}
	user.AgentCLICredentials[runtime.AgentCLIOpenCode] = domain.AgentCLICredential{
		APIKey: credential.APIKey, APIKeyConfigured: strings.TrimSpace(credential.APIKey) != "",
		BaseURL: credential.BaseURL, Protocol: credential.Protocol, Models: credential.Models,
	}
	return user
}

func errAssistantOpenCodeSponsoredModel(models []string) string {
	return "未配置个人 OpenCode API Key 时只能使用平台管理员允许的模型（" + strings.Join(models, "、") + "）。要使用其他模型，请先在个人配置中配置自己的 OpenCode API Key"
}
