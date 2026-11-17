package repo

import (
	"testing"

	"agentflow/internal/domain"
)

func TestAnalyticsPricedRunIncludesSubscriptionNodes(t *testing.T) {
	run := domain.Run{
		ID: "included", Status: "succeeded",
		Usage: domain.TokenUsage{InputTokens: 2e6, OutputTokens: 2e6, TotalTokens: 4e6, CostUSD: 2, CostStatus: "reported"},
		Nodes: map[string]*domain.NodeRun{
			"sub": {Model: "gpt-5.5", Provider: "codex", AuthMode: domain.CodexAuthSubscription, Usage: domain.TokenUsage{InputTokens: 1e6, OutputTokens: 1e6, TotalTokens: 2e6, CostUSD: 1, CostStatus: "reported"}},
			"api": {Model: "gpt-5.5", Provider: "codex", AuthMode: domain.CodexAuthAPIKey, Usage: domain.TokenUsage{InputTokens: 1e6, OutputTokens: 1e6, TotalTokens: 2e6, CostUSD: 1, CostStatus: "reported"}},
		},
	}
	priced := analyticsPricedRun(run)
	if got := priced.Nodes["sub"].Usage; got.CostUSD != 0 || got.CostStatus != "included" {
		t.Fatalf("subscription node = %+v", got)
	}
	if priced.Usage.CostStatus != "estimated" {
		t.Fatalf("mixed run status = %q", priced.Usage.CostStatus)
	}
	run.Nodes = map[string]*domain.NodeRun{
		"sub":    {Model: "gpt-5.5", Provider: "codex", AuthMode: domain.CodexAuthGateway, Usage: domain.TokenUsage{InputTokens: 1e6, OutputTokens: 1e6, TotalTokens: 2e6, CostUSD: 1, CostStatus: "reported"}},
		"script": {},
	}
	run.Usage = domain.TokenUsage{InputTokens: 1e6, OutputTokens: 1e6, TotalTokens: 2e6, CostUSD: 1, CostStatus: "reported"}
	priced = analyticsPricedRun(run)
	if priced.Usage.CostStatus != "included" || priced.Usage.CostUSD != 0 {
		t.Fatalf("all-included run = %+v", priced.Usage)
	}
}

func TestCombineCostStatus(t *testing.T) {
	for _, tc := range []struct{ a, b, want string }{
		{"included", "included", "included"},
		{"included", "estimated", "estimated"},
		{"included", "unknown", "unknown"},
		{"estimated", "unknown", "unknown"},
		{"reported", "estimated", "estimated"},
		{"estimated", "reported", "estimated"},
		{"reported", "included", "estimated"},
		{"included", "reported", "estimated"},
		{"", "included", "included"},
	} {
		if got := domain.CombineCostStatus(tc.a, tc.b); got != tc.want {
			t.Fatalf("CombineCostStatus(%q,%q)=%q, want %q", tc.a, tc.b, got, tc.want)
		}
	}
}
