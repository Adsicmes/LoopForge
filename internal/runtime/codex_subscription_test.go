package runtime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agentflow/internal/domain"
)

const codexTestAuthJSON = `{"auth_mode":"chatgpt","tokens":{"id_token":{"email":"dev@example.com","chatgpt_plan_type":"plus","chatgpt_account_id":"acct-1"},"access_token":"a","refresh_token":"r","account_id":"acct-1"},"last_refresh":"2026-01-01T00:00:00Z"}`

func TestWriteCodexAuthFileNewestWins(t *testing.T) {
	workspace := t.TempDir()
	path := filepath.Join(workspace, ".agentflow", "private-home", "codex", ".codex", "auth.json")
	if err := WriteCodexAuthFile(workspace, path, []byte(codexTestAuthJSON)); err != nil {
		t.Fatal(err)
	}
	newer := strings.Replace(codexTestAuthJSON, "2026-01-01", "2026-02-01", 1)
	if err := WriteCodexAuthFile(workspace, path, []byte(newer)); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "2026-02-01") {
		t.Fatalf("newer auth.json not written: %s", got)
	}
	older := strings.Replace(codexTestAuthJSON, "2026-01-01", "2025-12-01", 1)
	if err := WriteCodexAuthFile(workspace, path, []byte(older)); err != nil {
		t.Fatal(err)
	}
	got, _ = os.ReadFile(path)
	if !strings.Contains(string(got), "2026-02-01") {
		t.Fatalf("older auth.json overwrote newer: %s", got)
	}
}

func TestCodexGatewayProviderArgv(t *testing.T) {
	got, err := CodexGatewayProviderArgv([]string{"codex", "exec", "prompt"}, "https://gw.example/api/codex-gateway")
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(got, " ")
	if !strings.Contains(joined, `model_provider="agentflow_gateway"`) || !strings.Contains(joined, "https://gw.example") || got[len(got)-1] != "prompt" {
		t.Fatalf("argv = %v", got)
	}
}

func TestBindCodexSubscription(t *testing.T) {
	workspace := t.TempDir()
	command, err := providerForKind("codex").Prepare(providerInvocation{Executor: domain.ExecutorDefinition{Binary: "codex"}, Prompt: "test", Driver: "local"})
	if err != nil {
		t.Fatal(err)
	}
	bound, err := bindCommand(&localSandbox{workspace: workspace}, workspace, command, RunCredentials{Codex: ProviderCredential{AuthMode: domain.CodexAuthSubscription, AuthJSON: []byte(codexTestAuthJSON), AuthGeneration: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if bound.environment["CODEX_API_KEY"] != "" {
		t.Fatal("subscription mode kept an API key")
	}
	if !strings.Contains(strings.Join(bound.Argv, " "), "cli_auth_credentials_store") {
		t.Fatal("file credential store override missing")
	}
	if strings.Contains(strings.Join(bound.Argv, " "), "agentflow_user") {
		t.Fatal("subscription mode injected the API-key provider")
	}
	home := bound.environment["HOME"]
	authPath := filepath.Join(home, ".codex", "auth.json")
	if _, err := os.Stat(authPath); err != nil {
		t.Fatalf("auth.json missing at %s: %v", authPath, err)
	}
}

func TestBindCodexGateway(t *testing.T) {
	workspace := t.TempDir()
	command, err := providerForKind("codex").Prepare(providerInvocation{Executor: domain.ExecutorDefinition{Binary: "codex"}, Prompt: "test", Driver: "local"})
	if err != nil {
		t.Fatal(err)
	}
	bound, err := bindCommand(&localSandbox{workspace: workspace}, workspace, command, RunCredentials{Codex: ProviderCredential{
		AuthMode:     domain.CodexAuthGateway,
		GatewayURL:   "https://gw.example/api/codex-gateway",
		GatewayToken: "run-token",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if bound.environment["AGENTFLOW_CODEX_GATEWAY_TOKEN"] != "run-token" {
		t.Fatal("gateway token not injected")
	}
	if bound.environment["CODEX_API_KEY"] != "" {
		t.Fatal("gateway mode kept an API key")
	}
	if !strings.Contains(strings.Join(bound.Argv, " "), "agentflow_gateway") {
		t.Fatal("gateway provider missing")
	}
	if _, err := os.Stat(filepath.Join(bound.environment["HOME"], ".codex", "auth.json")); !os.IsNotExist(err) {
		t.Fatal("gateway mode wrote auth.json")
	}
}

func TestBindCodexAuthError(t *testing.T) {
	workspace := t.TempDir()
	command, err := providerForKind("codex").Prepare(providerInvocation{Executor: domain.ExecutorDefinition{Binary: "codex"}, Prompt: "test", Driver: "local"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = bindCommand(&localSandbox{workspace: workspace}, workspace, command, RunCredentials{Codex: ProviderCredential{AuthMode: domain.CodexAuthSubscription, AuthError: "订阅已失效"}})
	if err == nil || !strings.Contains(err.Error(), "订阅已失效") {
		t.Fatalf("error = %v", err)
	}
}
