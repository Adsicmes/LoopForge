package domain

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

const validCodexAuthJSON = `{
  "auth_mode": "chatgpt",
  "tokens": {
    "id_token": {
      "email": "dev@example.com",
      "chatgpt_plan_type": "plus",
      "chatgpt_user_id": "user-1",
      "chatgpt_account_id": "acct-1",
      "raw_jwt": ""
    },
    "access_token": "access-token",
    "refresh_token": "refresh-token",
    "account_id": "acct-1"
  },
  "last_refresh": "2026-11-17T08:00:00Z"
}`

func TestParseCodexAuthJSON(t *testing.T) {
	file, err := ParseCodexAuthJSON([]byte(validCodexAuthJSON))
	if err != nil {
		t.Fatal(err)
	}
	meta, err := file.Metadata()
	if err != nil {
		t.Fatal(err)
	}
	if meta.Email != "dev@example.com" || meta.PlanType != "Plus" || meta.AccountID != "acct-1" {
		t.Fatalf("metadata = %+v", meta)
	}
	if file.LastRefresh == nil || file.LastRefresh.IsZero() {
		t.Fatal("last_refresh was not parsed")
	}
}

func TestParseCodexAuthJSONRejectsAPIKeyFile(t *testing.T) {
	_, err := ParseCodexAuthJSON([]byte(`{"OPENAI_API_KEY":"sk-test"}`))
	if err == nil || !strings.Contains(err.Error(), "API Key") {
		t.Fatalf("error = %v", err)
	}
}

func TestParseCodexAuthJSONRequiresTokens(t *testing.T) {
	_, err := ParseCodexAuthJSON([]byte(`{"auth_mode":"chatgpt","tokens":{"access_token":"a"}}`))
	if err == nil || !strings.Contains(err.Error(), "refresh_token") {
		t.Fatalf("error = %v", err)
	}
}

func TestParseCodexAuthJSONAcceptsRawStringIDToken(t *testing.T) {
	claims := map[string]any{
		"email": "old@example.com",
		"https://api.openai.com/auth": map[string]any{
			"chatgpt_plan_type":  "pro",
			"chatgpt_account_id": "acct-old",
		},
	}
	payload, _ := json.Marshal(claims)
	jwt := "header." + base64.RawURLEncoding.EncodeToString(payload) + ".sig"
	raw := `{"tokens":{"id_token":"` + jwt + `","access_token":"a","refresh_token":"r"}}`
	file, err := ParseCodexAuthJSON([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	meta, err := file.Metadata()
	if err != nil {
		t.Fatal(err)
	}
	if meta.Email != "old@example.com" || meta.PlanType != "Pro" || meta.AccountID != "acct-old" {
		t.Fatalf("metadata = %+v", meta)
	}
}

func TestParseCodexAuthJSONRejectsOtherAuthMode(t *testing.T) {
	raw := strings.Replace(validCodexAuthJSON, `"auth_mode": "chatgpt"`, `"auth_mode": "apiKey"`, 1)
	if _, err := ParseCodexAuthJSON([]byte(raw)); err == nil {
		t.Fatal("apiKey auth_mode was accepted")
	}
}

func TestCodexAuthNeedsReauth(t *testing.T) {
	for _, text := range []string{
		"Your access token could not be refreshed because your refresh token was already used. Please log out and sign in again.",
		"refresh_token_expired",
		"refresh_token_invalidated",
	} {
		if !CodexAuthNeedsReauth(text) {
			t.Fatalf("not recognized: %s", text)
		}
	}
	if CodexAuthNeedsReauth("network timeout") {
		t.Fatal("transient error was classified as reauth")
	}
}

func TestDisplayCodexPlan(t *testing.T) {
	for raw, want := range map[string]string{
		"plus": "Plus", "pro": "Pro", "business": "Business", "enterprise": "Enterprise",
		"self_serve_business_usage_based": "Business", "edu": "Edu", "unknown": "unknown",
	} {
		if got := DisplayCodexPlan(raw); got != want {
			t.Fatalf("DisplayCodexPlan(%q) = %q, want %q", raw, got, want)
		}
	}
}
