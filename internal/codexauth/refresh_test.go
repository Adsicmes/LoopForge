package codexauth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"agentflow/internal/domain"
	"agentflow/internal/repo"
)

func testSecret(t *testing.T) {
	t.Helper()
	t.Setenv(GatewaySecretEnv, "test-gateway-secret")
	t.Setenv(domain.CredentialKeyEnv, base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef")))
}

func testJWT(exp int64) string {
	payload, _ := json.Marshal(map[string]int64{"exp": exp})
	return "header." + base64.RawURLEncoding.EncodeToString(payload) + ".sig"
}

func testAuthJSON(access, refresh string, exp int64) []byte {
	return []byte(`{"auth_mode":"chatgpt","tokens":{"id_token":{"email":"dev@example.com","chatgpt_plan_type":"plus","chatgpt_account_id":"acct-1"},"access_token":"` + access + `","refresh_token":"` + refresh + `","account_id":"acct-1"},"last_refresh":"2026-01-01T00:00:00Z"}`)
}

func testUser(t *testing.T, s *repo.Store) domain.User {
	t.Helper()
	if err := s.PutUser(domain.User{LoginName: "gateway-user"}); err != nil {
		t.Fatal(err)
	}
	user, ok := s.UserByLoginName("gateway-user")
	if !ok {
		t.Fatal("user not created")
	}
	return user
}

func TestGatewayTokenRoundTrip(t *testing.T) {
	testSecret(t)
	token, expires, err := Issue(42, "run-1", "session-1", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if time.Until(expires) <= 0 {
		t.Fatal("token already expired")
	}
	claims, err := Verify(token)
	if err != nil {
		t.Fatal(err)
	}
	if claims.UserID != 42 || claims.RunID != "run-1" || claims.SessionID != "session-1" {
		t.Fatalf("claims = %+v", claims)
	}
	parts := strings.Split(token, ".")
	parts[1] = base64.RawURLEncoding.EncodeToString([]byte("tampered"))
	tampered := strings.Join(parts, ".")
	if _, err := Verify(tampered); err == nil {
		t.Fatal("tampered token accepted")
	}
}

func TestAccessTokenRefreshes(t *testing.T) {
	testSecret(t)
	s, err := repo.OpenTest()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	user := testUser(t, s)
	if _, err := s.SaveCodexSubscriptionAndMode(user.ID, testAuthJSON(testJWT(1), "old-refresh", 1)); err != nil {
		t.Fatal(err)
	}

	var gotRefresh string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotRefresh = body["refresh_token"]
		if body["grant_type"] != "refresh_token" || body["client_id"] == "" {
			t.Errorf("refresh body = %+v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"` + testJWT(time.Now().Add(time.Hour).Unix()) + `","refresh_token":"new-refresh"}`))
	}))
	defer server.Close()
	t.Setenv("AGENTFLOW_CODEX_TOKEN_URL", server.URL)

	authJSON, access, account, err := AccessToken(context.Background(), s, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotRefresh != "old-refresh" {
		t.Fatalf("refresh token sent = %q", gotRefresh)
	}
	if !strings.Contains(string(authJSON), "new-refresh") || access == "" || account != "acct-1" {
		t.Fatalf("authJSON=%s access=%q account=%q", authJSON, access, account)
	}
	sub, ok := s.CodexSubscription(user.ID)
	if !ok || sub.Generation != 2 {
		t.Fatalf("stored subscription = %+v ok=%v", sub, ok)
	}
}

func TestAccessTokenReauthOnReusedToken(t *testing.T) {
	testSecret(t)
	s, err := repo.OpenTest()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	user := testUser(t, s)
	if _, err := s.SaveCodexSubscriptionAndMode(user.ID, testAuthJSON(testJWT(1), "old-refresh", 1)); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"code":"refresh_token_reused"}}`))
	}))
	defer server.Close()
	t.Setenv("AGENTFLOW_CODEX_TOKEN_URL", server.URL)

	_, _, _, err = AccessToken(context.Background(), s, user.ID)
	var reauth *ReauthError
	if err == nil || !strings.Contains(err.Error(), "重新连接") {
		t.Fatalf("error = %v", err)
	}
	if _, ok := err.(*ReauthError); !ok {
		t.Fatalf("error type = %T", reauth)
	}
	status, ok := s.CodexSubscriptionStatus(user.ID)
	if !ok || !status.NeedsReauth {
		t.Fatalf("status = %+v ok=%v", status, ok)
	}
}

func TestAccessTokenUsesFreshToken(t *testing.T) {
	testSecret(t)
	s, err := repo.OpenTest()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	user := testUser(t, s)
	fresh := testJWT(time.Now().Add(time.Hour).Unix())
	if _, err := s.SaveCodexSubscriptionAndMode(user.ID, testAuthJSON(fresh, "refresh", 1)); err != nil {
		t.Fatal(err)
	}
	authJSON, access, _, err := AccessToken(context.Background(), s, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if access != fresh || !strings.Contains(string(authJSON), fresh) {
		t.Fatalf("fresh token not used: %q", access)
	}
}
