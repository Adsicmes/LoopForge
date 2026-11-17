package httpapi

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"agentflow/internal/codexauth"
	"agentflow/internal/domain"
	"agentflow/internal/repo"

	"github.com/gin-gonic/gin"
)

func gatewayTestJWT(exp int64) string {
	payload, _ := json.Marshal(map[string]int64{"exp": exp})
	return "header." + base64.RawURLEncoding.EncodeToString(payload) + ".sig"
}

func TestCodexGatewayInjectsOAuthToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv(domain.CredentialKeyEnv, base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef")))
	t.Setenv(codexauth.GatewaySecretEnv, "gateway-test-secret")
	s, err := repo.OpenTest()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.SetCodexSubscriptionEnabled(true); err != nil {
		t.Fatal(err)
	}
	if err := s.PutUser(domain.User{LoginName: "gateway-chat"}); err != nil {
		t.Fatal(err)
	}
	user, ok := s.UserByLoginName("gateway-chat")
	if !ok {
		t.Fatal("user not created")
	}
	access := gatewayTestJWT(time.Now().Add(time.Hour).Unix())
	authJSON := []byte(`{"auth_mode":"chatgpt","tokens":{"id_token":{"email":"dev@example.com","chatgpt_plan_type":"plus","chatgpt_account_id":"acct-1"},"access_token":"` + access + `","refresh_token":"refresh","account_id":"acct-1"},"last_refresh":"2026-01-01T00:00:00Z"}`)
	if _, err := s.SaveCodexSubscriptionAndMode(user.ID, authJSON); err != nil {
		t.Fatal(err)
	}

	var gotAuth, gotAccount, gotBody string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotAccount = r.Header.Get("chatgpt-account-id")
		buf := make([]byte, 1024)
		n, _ := r.Body.Read(buf)
		gotBody = string(buf[:n])
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: ok\n\n"))
	}))
	defer upstream.Close()
	t.Setenv("AGENTFLOW_CODEX_UPSTREAM", upstream.URL)

	h := newCodexSubscriptionHandlers(&deps{repo: s})
	router := gin.New()
	router.Any("/api/codex-gateway/*path", h.gateway)
	token, _, err := codexauth.Issue(user.ID, "", "chat-1", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/codex-gateway/responses", strings.NewReader(`{"model":"gpt"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if gotAuth != "Bearer "+access || gotAccount != "acct-1" || gotBody != `{"model":"gpt"}` {
		t.Fatalf("upstream auth=%q account=%q body=%q", gotAuth, gotAccount, gotBody)
	}
	if rec.Body.String() != "data: ok\n\n" {
		t.Fatalf("stream body = %q", rec.Body.String())
	}
}

func TestCodexGatewayRejectsBadTokenAndPath(t *testing.T) {
	gin.SetMode(gin.TestMode)
	s, err := repo.OpenTest()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	h := newCodexSubscriptionHandlers(&deps{repo: s})
	router := gin.New()
	router.Any("/api/codex-gateway/*path", h.gateway)
	req := httptest.NewRequest(http.MethodPost, "/api/codex-gateway/responses", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer bad")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("bad token status = %d", rec.Code)
	}
}
