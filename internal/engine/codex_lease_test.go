package engine

import (
	"context"
	"encoding/base64"
	"testing"
	"time"

	"agentflow/internal/domain"
	"agentflow/internal/repo"
	"agentflow/internal/runtime"
)

func TestCodexRunLeaseSerializesPlatformRuns(t *testing.T) {
	t.Setenv(domain.CredentialKeyEnv, base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef")))
	s, err := repo.OpenTest()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.PutUser(domain.User{LoginName: "codex-lease-owner"}); err != nil {
		t.Fatal(err)
	}
	user, ok := s.UserByLoginName("codex-lease-owner")
	if !ok {
		t.Fatal("user not created")
	}
	authJSON := []byte(`{"auth_mode":"chatgpt","tokens":{"id_token":{"email":"lease@example.com","chatgpt_plan_type":"plus","chatgpt_account_id":"acct-1"},"access_token":"access","refresh_token":"refresh"},"last_refresh":"2026-01-01T00:00:00Z"}`)
	if _, err := s.SaveCodexSubscriptionAndMode(user.ID, authJSON); err != nil {
		t.Fatal(err)
	}
	e := &Engine{repo: s}
	cred := runtime.ProviderCredential{AuthMode: domain.CodexAuthSubscription}

	release, err := e.acquireCodexRunLease(context.Background(), user.ID, "run-a", cred, nil)
	if err != nil {
		t.Fatal(err)
	}
	waitCtx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	if _, err := e.acquireCodexRunLease(waitCtx, user.ID, "run-b", cred, nil); err == nil {
		t.Fatal("a second platform run acquired a held subscription lease")
	}
	release()
	second, err := e.acquireCodexRunLease(context.Background(), user.ID, "run-b", cred, nil)
	if err != nil {
		t.Fatal(err)
	}
	second()

	noop, err := e.acquireCodexRunLease(context.Background(), user.ID, "run-c", runtime.ProviderCredential{AuthMode: domain.CodexAuthAPIKey}, nil)
	if err != nil {
		t.Fatal(err)
	}
	noop()
}

func TestCodexRunLeaseFailsWhenSubscriptionDisconnected(t *testing.T) {
	t.Setenv(domain.CredentialKeyEnv, base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef")))
	s, err := repo.OpenTest()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.PutUser(domain.User{LoginName: "codex-lease-gone"}); err != nil {
		t.Fatal(err)
	}
	user, _ := s.UserByLoginName("codex-lease-gone")
	e := &Engine{repo: s}
	waitCtx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	_, err = e.acquireCodexRunLease(waitCtx, user.ID, "run-a", runtime.ProviderCredential{AuthMode: domain.CodexAuthSubscription}, nil)
	if err == nil || err == context.DeadlineExceeded {
		t.Fatalf("a missing subscription should fail explicitly, got %v", err)
	}
}
