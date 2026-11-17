package httpapi

import (
	"encoding/base64"
	"testing"

	"agentflow/internal/codexauth"
	"agentflow/internal/domain"
	"agentflow/internal/repo"
)

func codexAuthTestJSON() []byte {
	return []byte(`{"auth_mode":"chatgpt","tokens":{"id_token":{"email":"dev@example.com","chatgpt_plan_type":"plus","chatgpt_account_id":"acct-1"},"access_token":"access","refresh_token":"refresh","account_id":"acct-1"},"last_refresh":"2026-01-01T00:00:00Z"}`)
}

func TestCodexSubscriptionSelected(t *testing.T) {
	s, err := repo.OpenTest()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.SetCodexSubscriptionEnabled(true); err != nil {
		t.Fatal(err)
	}
	h := &assistantHandlers{deps: &deps{repo: s}}

	subscribed := domain.User{ID: 1, CodexSubscriptionConfigured: true}
	if !h.codexSubscriptionSelected(subscribed) {
		t.Fatal("subscription without an API key should select the subscription")
	}
	both := subscribed
	both.CodexAPIKey = "sk-key"
	if h.codexSubscriptionSelected(both) {
		t.Fatal("an API key should keep the API-key catalog when no mode is chosen")
	}
	both.CodexAuthMode = domain.CodexAuthSubscription
	if !h.codexSubscriptionSelected(both) {
		t.Fatal("the explicit subscription choice must win over a saved API key")
	}
	both.CodexAuthMode = domain.CodexAuthAPIKey
	if h.codexSubscriptionSelected(both) {
		t.Fatal("the explicit API-key choice must not expose the subscription catalog")
	}
	if err := s.SetCodexSubscriptionEnabled(false); err != nil {
		t.Fatal(err)
	}
	if h.codexSubscriptionSelected(subscribed) {
		t.Fatal("a disabled platform switch must hide the subscription catalog")
	}
}

func TestCodexCredentialFollowsExplicitMode(t *testing.T) {
	t.Setenv(domain.CredentialKeyEnv, base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef")))
	t.Setenv(codexauth.GatewaySecretEnv, "codex-auth-test-secret")
	t.Setenv("AGENTFLOW_PUBLIC_URL", "https://loopforge.example")
	s, err := repo.OpenTest()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.SetCodexSubscriptionEnabled(true); err != nil {
		t.Fatal(err)
	}
	if err := s.PutUser(domain.User{LoginName: "codex-auth-owner"}); err != nil {
		t.Fatal(err)
	}
	user, ok := s.UserByLoginName("codex-auth-owner")
	if !ok {
		t.Fatal("user not created")
	}
	if _, err := s.SaveCodexSubscriptionAndMode(user.ID, codexAuthTestJSON()); err != nil {
		t.Fatal(err)
	}
	h := &assistantHandlers{deps: &deps{repo: s}}

	cred := h.codexCredential(user, false, "")
	if cred == nil || cred.AuthMode != domain.CodexAuthSubscription || len(cred.AuthJSON) == 0 || cred.AuthGeneration <= 0 {
		t.Fatalf("local subscription credential = %+v", cred)
	}

	apiKeyOnly := user
	apiKeyOnly.CodexAuthMode = domain.CodexAuthAPIKey
	apiKeyOnly.CodexAPIKey = ""
	cred = h.codexCredential(apiKeyOnly, false, "")
	if cred == nil || cred.AuthMode != domain.CodexAuthAPIKey || cred.AuthError == "" || len(cred.AuthJSON) != 0 {
		t.Fatalf("explicit API-key mode fell back to the subscription: %+v", cred)
	}

	remote := h.codexCredential(user, true, "chat-1")
	if remote == nil || remote.AuthMode != domain.CodexAuthGateway || remote.GatewayToken == "" || remote.GatewayURL == "" {
		t.Fatalf("remote subscription credential = %+v", remote)
	}
	if len(remote.AuthJSON) != 0 {
		t.Fatal("gateway credential must not carry auth.json")
	}
	claims, err := codexauth.Verify(remote.GatewayToken)
	if err != nil {
		t.Fatal(err)
	}
	if claims.UserID != user.ID || claims.SessionID != "chat-1" {
		t.Fatalf("gateway claims = %+v", claims)
	}
}

func TestCodexSubscriptionModelsIgnoreCustomCatalog(t *testing.T) {
	user := domain.User{
		CodexAPIKey:  "sk-key",
		CodexBaseURL: "https://api.example/v1",
		CodexModels:  []domain.CLIModel{{ID: "custom-model", Name: "Custom"}},
	}
	foundBuiltin := false
	for _, model := range assistantEngineModels(t.Context(), user, true) {
		if model.Engine != assistantEngineCodex {
			continue
		}
		if model.ID == "codex:custom-model" {
			t.Fatal("subscription mode exposed the API-key custom catalog")
		}
		foundBuiltin = true
	}
	if !foundBuiltin {
		t.Fatal("subscription mode listed no Codex models")
	}
}
