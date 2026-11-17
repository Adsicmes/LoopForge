package httpapi

import (
	"strings"
	"testing"

	"agentflow/internal/domain"
	"agentflow/internal/repo"
)

func TestAssistantTurnUserUsesSharedOpenCode(t *testing.T) {
	s, err := repo.OpenTest()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	h := &assistantHandlers{deps: &deps{repo: s}}
	if err := s.SetPlatformOpenCodeKey(repo.PlatformOpenCodeKeyUpdate{
		APIKey: "sk_shared", BaseURL: "https://gw.example", Protocol: "chat", Models: []string{"glm-5", "kimi-k2"},
	}, 1); err != nil {
		t.Fatal(err)
	}
	user, model, refusal := h.assistantTurnUser(domain.User{ID: 7, LoginName: "member"}, "opencode:glm-5")
	if refusal != "" {
		t.Fatalf("refusal: %s", refusal)
	}
	credential := user.AgentCLICredentials["opencode"]
	if model != "opencode:glm-5" || credential.APIKey != "sk_shared" || credential.BaseURL != "https://gw.example" || credential.Protocol != "chat" {
		t.Fatalf("turn = %q %+v", model, credential)
	}
	if _, _, refusal := h.assistantTurnUser(domain.User{ID: 7}, "opencode:not-allowed"); refusal == "" || !strings.Contains(refusal, "glm-5") {
		t.Fatalf("out-of-list model accepted: %q", refusal)
	}
	if _, _, refusal := h.assistantTurnUser(domain.User{ID: 7}, "opencode:default"); refusal != "" {
		t.Fatalf("default should resolve to the first shared model: %s", refusal)
	}
}

func TestAssistantTurnUserRequiresOpenCodeCredential(t *testing.T) {
	s, err := repo.OpenTest()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	h := &assistantHandlers{deps: &deps{repo: s}}
	if _, _, refusal := h.assistantTurnUser(domain.User{ID: 8}, "opencode:glm-5"); refusal == "" || !strings.Contains(refusal, "OpenCode") {
		t.Fatalf("expected a missing-credential error, got %q", refusal)
	}
}
