package engine

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agentflow/internal/domain"
	"agentflow/internal/repo"
	"agentflow/internal/runtime"
)

func codexTestAuthJSON(email, access string) []byte {
	return []byte(fmt.Sprintf(`{"auth_mode":"chatgpt","tokens":{"id_token":{"email":%q,"chatgpt_plan_type":"plus","chatgpt_account_id":"acct-1"},"access_token":%q,"refresh_token":"refresh-token","account_id":"acct-1"},"last_refresh":"2026-11-17T08:00:00Z"}`, email, access))
}

func codexTestStore(t *testing.T) *repo.Store {
	t.Helper()
	t.Setenv(domain.CredentialKeyEnv, base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef")))
	s, err := repo.OpenTest()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}

func codexTestUser(t *testing.T, s *repo.Store, login string) domain.User {
	t.Helper()
	if err := s.PutUser(domain.User{LoginName: login}); err != nil {
		t.Fatal(err)
	}
	user, ok := s.UserByLoginName(login)
	if !ok {
		t.Fatal("user not created")
	}
	return user
}

func TestResolveCodexCredential(t *testing.T) {
	s := codexTestStore(t)
	e := &Engine{repo: s}
	user := codexTestUser(t, s, "codex-resolve")
	user.CodexAPIKey = "sk-test"
	user.CodexBaseURL = "https://api.example/v1"

	var nilEngine *Engine
	if got := nilEngine.resolveCodexCredential(user); got.APIKey != "sk-test" || got.AuthMode != "" {
		t.Fatalf("nil engine = %+v", got)
	}
	if got := (&Engine{}).resolveCodexCredential(user); got.APIKey != "sk-test" || got.AuthMode != "" {
		t.Fatalf("engine without repo = %+v", got)
	}
	if got := e.resolveCodexCredential(domain.User{CodexAPIKey: "sk-anon"}); got.APIKey != "sk-anon" || got.AuthMode != "" {
		t.Fatalf("anonymous user = %+v", got)
	}

	if got := e.resolveCodexCredential(user); got.AuthMode != domain.CodexAuthAPIKey || got.APIKey != "sk-test" || got.BaseURL != "https://api.example/v1" {
		t.Fatalf("api key = %+v", got)
	}
	user.CodexAuthMode = domain.CodexAuthAPIKey
	if got := e.resolveCodexCredential(domain.User{ID: user.ID, CodexAuthMode: domain.CodexAuthAPIKey}); got.AuthMode != domain.CodexAuthAPIKey || got.AuthError == "" {
		t.Fatalf("missing api key = %+v", got)
	}

	user.CodexSubscriptionConfigured = true
	user.CodexAuthMode = domain.CodexAuthSubscription
	if got := e.resolveCodexCredential(user); got.AuthMode != domain.CodexAuthSubscription || !strings.Contains(got.AuthError, "未开启") {
		t.Fatalf("disabled subscription = %+v", got)
	}
	if err := s.SetCodexSubscriptionEnabled(true); err != nil {
		t.Fatal(err)
	}
	if got := e.resolveCodexCredential(user); got.AuthMode != domain.CodexAuthSubscription || !strings.Contains(got.AuthError, "无法解密") {
		t.Fatalf("missing credential = %+v", got)
	}
	user.CodexSubscriptionConfigured = false
	if got := e.resolveCodexCredential(user); got.AuthMode != domain.CodexAuthSubscription || !strings.Contains(got.AuthError, "没有连接") {
		t.Fatalf("unconnected subscription = %+v", got)
	}

	saved, err := s.SaveCodexSubscriptionAndMode(user.ID, codexTestAuthJSON("resolve@example.com", "access-1"))
	if err != nil {
		t.Fatal(err)
	}
	user.CodexSubscriptionConfigured = true
	if got := e.resolveCodexCredential(user); got.AuthMode != domain.CodexAuthSubscription || string(got.AuthJSON) != string(codexTestAuthJSON("resolve@example.com", "access-1")) || got.AuthGeneration != saved.Generation {
		t.Fatalf("subscription = %+v", got)
	}
	if err := s.MarkCodexSubscriptionReauth(user.ID, saved.Generation, "refresh_token_expired"); err != nil {
		t.Fatal(err)
	}
	if got := e.resolveCodexCredential(user); got.AuthMode != domain.CodexAuthSubscription || !strings.Contains(got.AuthError, "登录已失效") {
		t.Fatalf("reauth = %+v", got)
	}

	user.CodexAuthMode = domain.CodexAuthAccessToken
	if got := e.resolveCodexCredential(user); got.AuthMode != domain.CodexAuthAccessToken || got.AuthError == "" {
		t.Fatalf("access token = %+v", got)
	}

	// With no explicit mode, an API key wins over a usable subscription.
	saved, err = s.SaveCodexSubscriptionAndMode(user.ID, codexTestAuthJSON("resolve2@example.com", "access-2"))
	if err != nil {
		t.Fatal(err)
	}
	noMode := domain.User{ID: user.ID, CodexAPIKey: "sk-fallback", CodexSubscriptionConfigured: true}
	if got := e.resolveCodexCredential(noMode); got.AuthMode != domain.CodexAuthAPIKey || got.APIKey != "sk-fallback" {
		t.Fatalf("fallback api key = %+v", got)
	}
	noMode.CodexAPIKey = ""
	if got := e.resolveCodexCredential(noMode); got.AuthMode != domain.CodexAuthSubscription || got.AuthGeneration != saved.Generation {
		t.Fatalf("fallback subscription = %+v", got)
	}
	if err := s.SetCodexSubscriptionEnabled(false); err != nil {
		t.Fatal(err)
	}
	if got := e.resolveCodexCredential(noMode); got.AuthMode != "" {
		t.Fatalf("disabled subscription fallback = %+v", got)
	}
}

func TestResolveCodexCredentialWithoutKeyConfig(t *testing.T) {
	t.Setenv(domain.CredentialKeyEnv, "")
	e := &Engine{repo: &repo.Store{}}
	for _, user := range []domain.User{
		{ID: 1, CodexAuthMode: domain.CodexAuthSubscription},
		{ID: 1, CodexSubscriptionConfigured: true},
	} {
		got := e.resolveCodexCredential(user)
		if got.AuthMode != domain.CodexAuthSubscription || !strings.Contains(got.AuthError, "AGENTFLOW_CREDENTIAL_KEY") {
			t.Fatalf("user %+v = %+v", user, got)
		}
	}
}

type codexTestSandbox struct {
	workspace string
	closed    bool
}

func (s *codexTestSandbox) ID() string        { return "codex-test" }
func (s *codexTestSandbox) Workspace() string { return s.workspace }
func (s *codexTestSandbox) Exec(context.Context, runtime.CommandSpec, runtime.EventSink) (string, error) {
	return "", nil
}
func (s *codexTestSandbox) Close(context.Context) error { s.closed = true; return nil }

func TestCaptureCodexSubscription(t *testing.T) {
	s := codexTestStore(t)
	user := codexTestUser(t, s, "codex-capture")
	saved, err := s.SaveCodexSubscriptionAndMode(user.ID, codexTestAuthJSON("old@example.com", "old-access"))
	if err != nil {
		t.Fatal(err)
	}
	workspace := t.TempDir()
	path := filepath.Join(workspace, ".agentflow", "private-home", "codex", ".codex", "auth.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	next := codexTestAuthJSON("new@example.com", "new-access")
	if err := os.WriteFile(path, next, 0o600); err != nil {
		t.Fatal(err)
	}

	sb := &runEnvironment{sandbox: &codexTestSandbox{workspace: workspace}}
	sb.setCredentials(runtime.RunCredentials{Codex: runtime.ProviderCredential{
		AuthMode:       domain.CodexAuthSubscription,
		AuthJSON:       saved.AuthJSON,
		AuthGeneration: saved.Generation,
	}})
	run := domain.Run{ExecutionContext: domain.TelemetryContext{ResponsibleUserID: user.ID}}

	var nilEngine *Engine
	nilEngine.captureCodexSubscription(run, sb)
	(&Engine{}).captureCodexSubscription(run, sb)
	(&Engine{repo: s}).captureCodexSubscription(run, nil)

	// A non-subscription credential leaves the file alone.
	sb.setCredentials(runtime.RunCredentials{Codex: runtime.ProviderCredential{AuthMode: domain.CodexAuthAPIKey}})
	(&Engine{repo: s}).captureCodexSubscription(run, sb)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file removed for an API key: %v", err)
	}

	// A missing private-home file is ignored.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	sb.setCredentials(runtime.RunCredentials{Codex: runtime.ProviderCredential{
		AuthMode: domain.CodexAuthSubscription, AuthGeneration: saved.Generation,
	}})
	(&Engine{repo: s}).captureCodexSubscription(run, sb)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("missing-file path recreated something: %v", err)
	}
	if err := os.WriteFile(path, next, 0o600); err != nil {
		t.Fatal(err)
	}

	// A stale generation is discarded and the file is kept for the next run.
	sb.setCredentials(runtime.RunCredentials{Codex: runtime.ProviderCredential{
		AuthMode: domain.CodexAuthSubscription, AuthGeneration: saved.Generation + 10,
	}})
	(&Engine{repo: s}).captureCodexSubscription(run, sb)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("stale capture removed the file: %v", err)
	}

	// A current generation is stored and the private token file is removed.
	sb.setCredentials(runtime.RunCredentials{Codex: runtime.ProviderCredential{
		AuthMode: domain.CodexAuthSubscription, AuthGeneration: saved.Generation,
	}})
	(&Engine{repo: s}).captureCodexSubscription(run, sb)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("file was not removed after capture: %v", err)
	}
	sub, ok := s.CodexSubscription(user.ID)
	if !ok || string(sub.AuthJSON) != string(next) || sub.Generation != saved.Generation+1 {
		t.Fatalf("captured subscription = %+v ok=%v", sub, ok)
	}
}
