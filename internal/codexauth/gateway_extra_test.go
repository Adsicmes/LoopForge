package codexauth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"agentflow/internal/domain"
	"agentflow/internal/repo"
)

func TestGatewayURL(t *testing.T) {
	cases := []struct {
		name    string
		gateway string
		public  string
		api     string
		want    string
	}{
		{name: "explicit", gateway: " https://gw.example/ ", want: "https://gw.example"},
		{name: "public", public: "https://app.example/", want: "https://app.example/api/codex-gateway"},
		{name: "api fallback", api: " https://api.example ", want: "https://api.example/api/codex-gateway"},
		{name: "invalid public", public: "::not-a-url", want: ""},
		{name: "hostless public", public: "https:///path", want: ""},
		{name: "empty", want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(GatewayURLEnv, tc.gateway)
			t.Setenv(PublicURLEnv, tc.public)
			t.Setenv(APIURLEnv, tc.api)
			if got := GatewayURL(); got != tc.want {
				t.Fatalf("GatewayURL() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestGatewaySecretFallback(t *testing.T) {
	t.Setenv(GatewaySecretEnv, "")
	t.Setenv("AGENTFLOW_SESSION_SECRET", "")
	if _, err := secret(); err == nil {
		t.Fatal("secret without configuration succeeded")
	}
	if _, _, err := Issue(1, "", "", time.Minute); err == nil {
		t.Fatal("Issue without configuration succeeded")
	}
	if _, err := Verify("a.b"); err == nil {
		t.Fatal("Verify without configuration succeeded")
	}

	t.Setenv(GatewaySecretEnv, "   ")
	t.Setenv("AGENTFLOW_SESSION_SECRET", "session-secret")
	token, _, err := Issue(7, "", "", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if claims, err := Verify(token); err != nil || claims.UserID != 7 {
		t.Fatalf("claims = %+v err = %v", claims, err)
	}
}

func TestIssueValidationAndTTLDefault(t *testing.T) {
	testSecret(t)
	if _, _, err := Issue(0, "run", "session", time.Minute); err == nil {
		t.Fatal("anonymous user obtained a gateway token")
	}
	token, expires, err := Issue(3, " run-1 ", " session-1 ", 0)
	if err != nil {
		t.Fatal(err)
	}
	if time.Until(expires) < time.Hour {
		t.Fatalf("default ttl is too short: %v", time.Until(expires))
	}
	claims, err := Verify(token)
	if err != nil {
		t.Fatal(err)
	}
	if claims.RunID != "run-1" || claims.SessionID != "session-1" || claims.Nonce == "" {
		t.Fatalf("claims = %+v", claims)
	}
}

func signGatewayPayload(t *testing.T, payload []byte) string {
	t.Helper()
	key, err := secret()
	if err != nil {
		t.Fatal(err)
	}
	body := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(body))
	return body + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func TestVerifyRejectsBrokenTokens(t *testing.T) {
	testSecret(t)
	for _, token := range []string{"", "only-one-part", "a.b.c", "a.!!!"} {
		if _, err := Verify(token); err == nil {
			t.Fatalf("accepted malformed token %q", token)
		}
	}
	future := time.Now().Add(time.Hour).Unix()
	for _, payload := range []string{
		"{",
		fmt.Sprintf(`{"uid":0,"exp":%d}`, future),
		`{"uid":1,"exp":0}`,
		`{"uid":1,"exp":1}`,
	} {
		token := signGatewayPayload(t, []byte(payload))
		if _, err := Verify(token); err == nil {
			t.Fatalf("accepted payload %s", payload)
		}
	}
	expired := signGatewayPayload(t, []byte(`{"uid":1,"exp":1}`))
	if _, err := Verify(expired); err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("expired error = %v", err)
	}
}

func testIDTokenJWT(accountID string) string {
	claims := map[string]any{
		"email": "refresh@example.com",
		"https://api.openai.com/auth": map[string]any{
			"chatgpt_plan_type":  "pro",
			"chatgpt_user_id":    "user-refresh",
			"chatgpt_account_id": accountID,
		},
	}
	payload, _ := json.Marshal(claims)
	return "header." + base64.RawURLEncoding.EncodeToString(payload) + ".sig"
}

func TestMergeAuthJSONBranches(t *testing.T) {
	if _, err := mergeAuthJSON([]byte("{"), "a", "", ""); err == nil {
		t.Fatal("invalid json was accepted")
	}
	if _, err := mergeAuthJSON([]byte(`{}`), "a", "", ""); err == nil {
		t.Fatal("missing tokens object was accepted")
	}
	existing := []byte(`{"tokens":{"access_token":"old","refresh_token":"old-refresh","account_id":""}}`)
	merged, err := mergeAuthJSON(existing, "new-access", "", "")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(merged, &doc); err != nil {
		t.Fatal(err)
	}
	tokens := doc["tokens"].(map[string]any)
	if tokens["access_token"] != "new-access" || tokens["refresh_token"] != "old-refresh" {
		t.Fatalf("tokens = %+v", tokens)
	}
	if _, ok := tokens["id_token"]; ok {
		t.Fatalf("id_token changed without a new token: %+v", tokens)
	}
	if tokens["account_id"] != "" {
		t.Fatalf("empty account id was replaced: %+v", tokens)
	}

	merged, err = mergeAuthJSON(existing, "new-access", "new-refresh", testIDTokenJWT("acct-new"))
	if err != nil {
		t.Fatal(err)
	}
	doc = map[string]any{}
	if err := json.Unmarshal(merged, &doc); err != nil {
		t.Fatal(err)
	}
	tokens = doc["tokens"].(map[string]any)
	if tokens["refresh_token"] != "new-refresh" || tokens["account_id"] != "acct-new" {
		t.Fatalf("tokens = %+v", tokens)
	}
	idToken, ok := tokens["id_token"].(map[string]any)
	if !ok || idToken["email"] != "refresh@example.com" || idToken["chatgpt_plan_type"] != "pro" || idToken["chatgpt_account_id"] != "acct-new" {
		t.Fatalf("id_token = %+v", tokens["id_token"])
	}
	if _, ok := doc["last_refresh"].(string); !ok {
		t.Fatalf("last_refresh = %+v", doc["last_refresh"])
	}

	merged, err = mergeAuthJSON([]byte(`{"tokens":{"access_token":"old","refresh_token":"old-r","account_id":"acct-old"}}`), "new-access", "", "not-a-jwt")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(merged), "acct-old") || strings.Contains(string(merged), "not-a-jwt") {
		t.Fatalf("invalid id_token changed the bundle: %s", merged)
	}
}

func TestValidAccessTokenAndJWTExpiry(t *testing.T) {
	if _, ok := validAccessToken(domain.CodexAuthFile{}, 0); ok {
		t.Fatal("file without tokens was accepted")
	}
	if _, ok := validAccessToken(domain.CodexAuthFile{Tokens: &domain.CodexAuthTokens{}}, 0); ok {
		t.Fatal("empty access token was accepted")
	}
	for _, token := range []string{"", "a", "a.b", "a..c", "a.!!!.c", testJWT(0)} {
		if _, ok := jwtExpiry(token); ok {
			t.Fatalf("malformed token %q was accepted", token)
		}
	}
	future := time.Now().Add(time.Hour).Unix()
	if exp, ok := jwtExpiry(testJWT(future)); !ok || exp.IsZero() {
		t.Fatalf("jwtExpiry = %v %v", exp, ok)
	}
	if token, ok := validAccessToken(domain.CodexAuthFile{Tokens: &domain.CodexAuthTokens{AccessToken: testJWT(future)}}, 0); !ok || token == "" {
		t.Fatal("fresh access token was rejected")
	}
	if _, ok := validAccessToken(domain.CodexAuthFile{Tokens: &domain.CodexAuthTokens{AccessToken: testJWT(1)}}, 0); ok {
		t.Fatal("expired access token was accepted")
	}
}

func TestRefreshRejectsInvalidResponses(t *testing.T) {
	testSecret(t)
	ctx := context.Background()
	if _, err := refresh(ctx, []byte("{")); err == nil {
		t.Fatal("invalid auth.json was accepted")
	}

	cases := []struct {
		name   string
		status int
		body   string
		reAuth bool
	}{
		{name: "server error", status: http.StatusInternalServerError, body: "boom"},
		{name: "invalid grant", status: http.StatusBadRequest, body: `{"error":"invalid_grant"}`, reAuth: true},
		{name: "invalid json", status: http.StatusOK, body: "not-json"},
		{name: "empty access token", status: http.StatusOK, body: `{}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			t.Setenv("AGENTFLOW_CODEX_TOKEN_URL", server.URL)
			_, err := refresh(ctx, testAuthJSON(testJWT(1), "refresh", 1))
			if err == nil {
				t.Fatal("refresh succeeded")
			}
			var reauth *ReauthError
			if errors.As(err, &reauth) != tc.reAuth {
				t.Fatalf("ReauthError = %v, want %v (err %v)", errors.As(err, &reauth), tc.reAuth, err)
			}
		})
	}
}

func TestRefreshMergesSuccessfulResponse(t *testing.T) {
	testSecret(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"` + testJWT(time.Now().Add(time.Hour).Unix()) + `","refresh_token":"next-refresh","id_token":"` + testIDTokenJWT("acct-refresh") + `"}`))
	}))
	defer server.Close()
	t.Setenv("AGENTFLOW_CODEX_TOKEN_URL", server.URL)
	merged, err := refresh(context.Background(), testAuthJSON(testJWT(1), "refresh", 1))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"next-refresh", "acct-refresh", "refresh@example.com"} {
		if !strings.Contains(string(merged), want) {
			t.Fatalf("merged auth.json missing %q: %s", want, merged)
		}
	}
}

func TestAccessTokenRequiresCredential(t *testing.T) {
	ctx := context.Background()
	if _, _, _, err := AccessToken(ctx, nil, 1); err == nil {
		t.Fatal("nil store accepted")
	}
	s, err := repo.OpenTest()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, _, _, err := AccessToken(ctx, s, 1); err == nil {
		t.Fatal("user without a subscription accepted")
	}
}

func TestRefreshRequestFailures(t *testing.T) {
	testSecret(t)
	ctx := context.Background()
	t.Setenv("AGENTFLOW_CODEX_TOKEN_URL", "://bad")
	if _, err := refresh(ctx, testAuthJSON(testJWT(1), "refresh", 1)); err == nil {
		t.Fatal("invalid token URL was accepted")
	}
	t.Setenv("AGENTFLOW_CODEX_TOKEN_URL", "http://127.0.0.1:1/")
	if _, err := refresh(ctx, testAuthJSON(testJWT(1), "refresh", 1)); err == nil {
		t.Fatal("unreachable token URL was accepted")
	}
}
