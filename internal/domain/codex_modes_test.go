package domain

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func TestValidCodexAuthMode(t *testing.T) {
	for _, raw := range []string{"", " ", CodexAuthAPIKey, " " + CodexAuthSubscription + " ", CodexAuthAccessToken} {
		if !ValidCodexAuthMode(raw) {
			t.Fatalf("ValidCodexAuthMode(%q) = false", raw)
		}
	}
	for _, raw := range []string{CodexAuthGateway, "apiKey", "oauth", "subscription_"} {
		if ValidCodexAuthMode(raw) {
			t.Fatalf("ValidCodexAuthMode(%q) = true", raw)
		}
	}
}

func TestParseCodexIDTokenJWT(t *testing.T) {
	claims := map[string]any{
		"email":                          "jwt@example.com",
		"https://api.openai.com/profile": map[string]any{"email": "profile@example.com"},
		"https://api.openai.com/auth": map[string]any{
			"chatgpt_plan_type":  "pro",
			"chatgpt_user_id":    "user-jwt",
			"chatgpt_account_id": "acct-jwt",
		},
	}
	payload, _ := json.Marshal(claims)
	jwt := "header." + base64.RawURLEncoding.EncodeToString(payload) + ".sig"
	got, err := ParseCodexIDTokenJWT(jwt)
	if err != nil {
		t.Fatal(err)
	}
	if got.Email != "jwt@example.com" || got.PlanType != "pro" || got.ChatGPTUserID != "user-jwt" || got.ChatGPTAccountID != "acct-jwt" || got.RawJWT != jwt {
		t.Fatalf("claims = %+v", got)
	}
	for _, raw := range []string{"", "a", "a.b", "a.!!!.c", "a." + base64.RawURLEncoding.EncodeToString([]byte("{")) + ".c"} {
		if _, err := ParseCodexIDTokenJWT(raw); err == nil {
			t.Fatalf("ParseCodexIDTokenJWT(%q) succeeded", raw)
		}
	}
}

func TestSupportsCodexGateway(t *testing.T) {
	cases := map[string]bool{
		"":        false,
		"0.10.9":  false,
		"0.11.0":  true,
		"0.11.1":  true,
		"1.0.0":   true,
		"v0.11.0": true,
	}
	for version, want := range cases {
		if got := (Runner{RunnerVersion: version}).SupportsCodexGateway(); got != want {
			t.Fatalf("SupportsCodexGateway(%q) = %v, want %v", version, got, want)
		}
	}
}

func TestIncludedAuthModeAndUsage(t *testing.T) {
	for _, mode := range []string{CodexAuthSubscription, CodexAuthGateway, CodexAuthAccessToken} {
		if !IsIncludedAuthMode(mode) {
			t.Fatalf("IsIncludedAuthMode(%q) = false", mode)
		}
	}
	for _, mode := range []string{"", CodexAuthAPIKey, "other"} {
		if IsIncludedAuthMode(mode) {
			t.Fatalf("IsIncludedAuthMode(%q) = true", mode)
		}
	}
	usage := IncludedUsage(TokenUsage{InputTokens: 10, OutputTokens: 2, CostUSD: 1.25, CostStatus: "estimated"})
	if usage.CostUSD != 0 || usage.CostStatus != "included" || usage.InputTokens != 10 || usage.OutputTokens != 2 {
		t.Fatalf("usage = %+v", usage)
	}
}

func TestDisplayCodexPlanAliases(t *testing.T) {
	for raw, want := range map[string]string{
		"":                                "",
		"  ":                              "",
		"PLUS":                            "Plus",
		"prolite":                         "Pro Lite",
		"promax":                          "Pro Max",
		"team":                            "Team",
		"self_serve_business_prolite":     "Business",
		"self_serve_business_usage_based": "Business",
		"hc":                              "Enterprise",
		"ent26":                           "Enterprise",
		"enterprise_cbp_automation":       "Enterprise",
		"enterprise_cbp_usage_based":      "Enterprise",
		"education":                       "Edu",
		"edu_plus":                        "Edu Plus",
		"edu_pro":                         "Edu Pro",
		"free":                            "Free",
		"go":                              "Go",
		" trial ":                         "trial",
	} {
		if got := DisplayCodexPlan(raw); got != want {
			t.Fatalf("DisplayCodexPlan(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestCodexAuthModeTrimming(t *testing.T) {
	if !ValidCodexAuthMode("\t" + CodexAuthAPIKey + "\n") {
		t.Fatal("mode with surrounding whitespace was rejected")
	}
	if IsIncludedAuthMode(" "+CodexAuthSubscription) == false {
		t.Fatal("included mode with whitespace was rejected")
	}
	if got := strings.TrimSpace(DisplayCodexPlan(" PLUS ")); got != "Plus" {
		t.Fatalf("plan = %q", got)
	}
}
