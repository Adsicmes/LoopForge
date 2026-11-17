package engine

import (
	"strings"
	"testing"

	"agentflow/internal/domain"
	"agentflow/internal/runtime"
)

func TestResolveOpenCodeExecutorModel(t *testing.T) {
	credential := runtime.ProviderCredential{APIKey: "k", Models: []domain.CLIModel{{ID: "glm-5"}, {ID: "kimi-k2"}}}
	credentials := runtime.RunCredentials{AgentCLI: map[string]runtime.ProviderCredential{"opencode": credential}}

	if got, err := resolveOpenCodeExecutorModel(credentials, domain.ExecutorDefinition{Kind: "opencode"}); err != nil || got.Model != "glm-5" {
		t.Fatalf("empty model = %+v %v", got, err)
	}
	if got, err := resolveOpenCodeExecutorModel(credentials, domain.ExecutorDefinition{Kind: "opencode", Model: "kimi-k2"}); err != nil || got.Model != "kimi-k2" {
		t.Fatalf("personal model = %+v %v", got, err)
	}
	if _, err := resolveOpenCodeExecutorModel(runtime.RunCredentials{}, domain.ExecutorDefinition{Kind: "opencode"}); err == nil || !strings.Contains(err.Error(), "OpenCode") {
		t.Fatalf("missing credential = %v", err)
	}

	sponsored := credentials
	sponsored.AgentCLI = map[string]runtime.ProviderCredential{"opencode": {APIKey: "shared", Sponsored: true, Models: []domain.CLIModel{{ID: "glm-5"}}}}
	if got, err := resolveOpenCodeExecutorModel(sponsored, domain.ExecutorDefinition{Kind: "opencode", Model: "glm-5"}); err != nil || got.Model != "glm-5" {
		t.Fatalf("allowed shared model = %+v %v", got, err)
	}
	if _, err := resolveOpenCodeExecutorModel(sponsored, domain.ExecutorDefinition{Kind: "opencode", Model: "kimi-k2"}); err == nil || !strings.Contains(err.Error(), "不允许") {
		t.Fatalf("disallowed shared model = %v", err)
	}
}
