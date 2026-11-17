package httpapi

import "testing"

func TestConsumeCodexLoginLineParsesDevicePrompt(t *testing.T) {
	flow := &codexDeviceFlow{status: "pending", ready: make(chan struct{})}
	lines := []string{
		"Welcome to Codex",
		"\x1b[1mOpen this link in your browser:\x1b[0m",
		"  https://auth.openai.com/codex/device",
		"Enter this one-time code (expires in 15 minutes):",
		"  abcd-1234",
	}
	h := &codexSubscriptionHandlers{}
	for _, line := range lines {
		h.consumeLoginLine(flow, line)
	}
	if flow.verificationURL != "https://auth.openai.com/codex/device" {
		t.Fatalf("verification URL = %q", flow.verificationURL)
	}
	if flow.userCode != "abcd-1234" {
		t.Fatalf("user code = %q", flow.userCode)
	}
	select {
	case <-flow.ready:
	default:
		t.Fatal("flow was not marked ready")
	}
}

func TestConsumeCodexLoginLineIgnoresProse(t *testing.T) {
	flow := &codexDeviceFlow{status: "pending", ready: make(chan struct{})}
	h := &codexSubscriptionHandlers{}
	for _, line := range []string{"Follow these steps to sign in with device-code", "Enter this one-time code", "2."} {
		h.consumeLoginLine(flow, line)
	}
	if flow.userCode != "" {
		t.Fatalf("prose was parsed as a device code: %q", flow.userCode)
	}
}
