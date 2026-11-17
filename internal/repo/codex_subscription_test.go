package repo

import (
	"encoding/base64"
	"testing"
	"time"

	"agentflow/internal/domain"
)

func testCodexCredentialKey(t *testing.T) {
	t.Helper()
	t.Setenv(domain.CredentialKeyEnv, base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef")))
}

func testCodexUser(t *testing.T, s *Store, login string) domain.User {
	t.Helper()
	if err := s.PutUser(domain.User{LoginName: login, ChineseName: login}); err != nil {
		t.Fatal(err)
	}
	user, ok := s.UserByLoginName(login)
	if !ok {
		t.Fatal("user not created")
	}
	return user
}

func testCodexAuthJSON(email string) []byte {
	return []byte(`{"auth_mode":"chatgpt","tokens":{"id_token":{"email":"` + email + `","chatgpt_plan_type":"plus","chatgpt_account_id":"acct-1"},"access_token":"access-token","refresh_token":"refresh-token","account_id":"acct-1"},"last_refresh":"2026-11-17T08:00:00Z"}`)
}

func TestCodexSubscriptionSaveAndCapture(t *testing.T) {
	testCodexCredentialKey(t)
	s, err := OpenTest()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	user := testCodexUser(t, s, "codex-owner")

	saved, err := s.SaveCodexSubscriptionAndMode(user.ID, testCodexAuthJSON("owner@example.com"))
	if err != nil {
		t.Fatal(err)
	}
	if saved.Generation != 1 || saved.Email != "owner@example.com" {
		t.Fatalf("saved = %+v", saved)
	}
	got, ok := s.CodexSubscription(user.ID)
	if !ok || string(got.AuthJSON) != string(testCodexAuthJSON("owner@example.com")) {
		t.Fatalf("credential = %+v ok=%v", got, ok)
	}
	stored, ok := s.User(user.ID)
	if !ok || stored.CodexAuthMode != domain.CodexAuthSubscription || !stored.CodexSubscriptionConfigured {
		t.Fatalf("user status = %+v ok=%v", stored, ok)
	}
	status, ok := s.CodexSubscriptionStatus(user.ID)
	if !ok || status.Email != "owner@example.com" || status.NeedsReauth {
		t.Fatalf("status = %+v ok=%v", status, ok)
	}

	// A newer generation can capture refreshed tokens.
	ok, err = s.CaptureCodexSubscription(user.ID, saved.Generation, testCodexAuthJSON("owner@example.com"))
	if err != nil || !ok {
		t.Fatalf("capture = %v %v", ok, err)
	}
	// A stale generation is discarded.
	ok, err = s.CaptureCodexSubscription(user.ID, saved.Generation, testCodexAuthJSON("old@example.com"))
	if err != nil || ok {
		t.Fatalf("stale capture = %v %v", ok, err)
	}
	if got, _ := s.CodexSubscription(user.ID); got.Generation != 2 {
		t.Fatalf("generation = %d, want 2", got.Generation)
	}
}

func TestCodexSubscriptionLeases(t *testing.T) {
	testCodexCredentialKey(t)
	s, err := OpenTest()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	user := testCodexUser(t, s, "codex-lease")
	if _, err := s.SaveCodexSubscriptionAndMode(user.ID, testCodexAuthJSON("lease@example.com")); err != nil {
		t.Fatal(err)
	}

	ok, err := s.AcquireCodexRunLease(user.ID, "run-a", time.Minute)
	if err != nil || !ok {
		t.Fatalf("first lease = %v %v", ok, err)
	}
	if ok, _ := s.AcquireCodexRunLease(user.ID, "run-b", time.Minute); ok {
		t.Fatal("second run acquired a held lease")
	}
	if err := s.ReleaseCodexRunLease(user.ID, "run-a"); err != nil {
		t.Fatal(err)
	}
	if ok, _ := s.AcquireCodexRunLease(user.ID, "run-b", time.Minute); !ok {
		t.Fatal("lease was not released")
	}

	if ok, _ := s.AcquireCodexRefreshLease(user.ID, "gw-a", 30*time.Second); !ok {
		t.Fatal("refresh lease not acquired")
	}
	if ok, _ := s.AcquireCodexRefreshLease(user.ID, "gw-b", 30*time.Second); ok {
		t.Fatal("second refresh lease acquired")
	}
	if err := s.ReleaseCodexRefreshLease(user.ID, "gw-a"); err != nil {
		t.Fatal(err)
	}
}

func TestCodexSubscriptionDeleteAndReauth(t *testing.T) {
	testCodexCredentialKey(t)
	s, err := OpenTest()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	user := testCodexUser(t, s, "codex-delete")
	saved, err := s.SaveCodexSubscriptionAndMode(user.ID, testCodexAuthJSON("delete@example.com"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.MarkCodexSubscriptionReauth(user.ID, saved.Generation, "refresh token was already used"); err != nil {
		t.Fatal(err)
	}
	status, ok := s.CodexSubscriptionStatus(user.ID)
	if !ok || !status.NeedsReauth {
		t.Fatalf("status = %+v ok=%v", status, ok)
	}
	if err := s.DeleteCodexSubscriptionAndMode(user.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.CodexSubscription(user.ID); ok {
		t.Fatal("credential still present after delete")
	}
	stored, _ := s.User(user.ID)
	if stored.CodexAuthMode != "" || stored.CodexSubscriptionConfigured {
		t.Fatalf("user status after delete = %+v", stored)
	}
}

func TestCodexSubscriptionRequiresEncryptionKey(t *testing.T) {
	t.Setenv(domain.CredentialKeyEnv, "")
	s, err := OpenTest()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	user := testCodexUser(t, s, "codex-nokey")
	if _, err := s.SaveCodexSubscriptionAndMode(user.ID, testCodexAuthJSON("nokey@example.com")); err == nil {
		t.Fatal("save without credential key succeeded")
	}
}

func TestPlatformSettings(t *testing.T) {
	s, err := OpenTest()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.CodexSubscriptionEnabled() {
		t.Fatal("codex subscription should default off")
	}
	if err := s.SetCodexSubscriptionEnabled(true); err != nil {
		t.Fatal(err)
	}
	if !s.CodexSubscriptionEnabled() {
		t.Fatal("setting was not persisted")
	}
	if err := s.SetCodexSubscriptionAuthJSONEnabled(true); err != nil {
		t.Fatal(err)
	}
	if !s.CodexSubscriptionAuthJSONEnabled() {
		t.Fatal("auth.json fallback setting was not persisted")
	}
}

func TestCodexSubscriptionMissingUser(t *testing.T) {
	testCodexCredentialKey(t)
	s, err := OpenTest()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, ok := s.CodexSubscriptionStatus(999); ok {
		t.Fatal("status for an unknown user")
	}
	if sub, ok := s.CodexSubscription(0); ok || len(sub.AuthJSON) != 0 {
		t.Fatalf("subscription for user 0 = %+v ok=%v", sub, ok)
	}
	if err := s.DeleteCodexSubscriptionAndMode(0); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.AcquireCodexRunLease(0, "owner", time.Minute); ok || err == nil {
		t.Fatalf("lease for user 0 = %v %v", ok, err)
	}
}

func TestCountCodexSubscriptions(t *testing.T) {
	testCodexCredentialKey(t)
	s, err := OpenTest()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if got := s.CountCodexSubscriptions(); got != 0 {
		t.Fatalf("initial count = %d", got)
	}
	first := testCodexUser(t, s, "codex-count-1")
	second := testCodexUser(t, s, "codex-count-2")
	if _, err := s.SaveCodexSubscriptionAndMode(first.ID, testCodexAuthJSON("one@example.com")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveCodexSubscriptionAndMode(second.ID, testCodexAuthJSON("two@example.com")); err != nil {
		t.Fatal(err)
	}
	if got := s.CountCodexSubscriptions(); got != 2 {
		t.Fatalf("count = %d, want 2", got)
	}
	if err := s.DeleteCodexSubscriptionAndMode(first.ID); err != nil {
		t.Fatal(err)
	}
	if got := s.CountCodexSubscriptions(); got != 1 {
		t.Fatalf("count after delete = %d, want 1", got)
	}
}

func TestScrubRunnerTaskCredentialsDropsCodexGateway(t *testing.T) {
	task := domain.RunnerTask{Invocation: &domain.RunnerInvocation{
		CodexAuthMode:         domain.CodexAuthGateway,
		CodexGatewayURL:       "https://loopforge.example/api/codex-gateway",
		CodexGatewayToken:     "run-token",
		CodexGatewayExpiresAt: time.Now().UTC().Add(time.Hour),
	}}
	scrubRunnerTaskCredentials(&task)
	if task.Invocation.CodexGatewayToken != "" || task.Invocation.CodexGatewayURL != "" || task.Invocation.CodexAuthMode != "" {
		t.Fatalf("gateway fields survived scrub: %+v", task.Invocation)
	}
}
