package runtime

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestOpenCodeV2CommandUsesStandaloneAndModelVariant(t *testing.T) {
	command := AgentCLICommand(AgentCLIOpenCode, AgentCLICommandOptions{
		Model: "glm-5", Effort: "high", Resume: "ses_1", Prompt: "hi", Chat: true,
	})
	joined := strings.Join(command.Argv, " ")
	for _, want := range []string{
		"run --standalone --auto --format json --thinking",
		"-m agentflow/glm-5#high",
		"-s ses_1",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("argv misses %q: %q", want, command.Argv)
		}
	}
	if strings.Contains(joined, "--variant") {
		t.Fatalf("v2 argv must not use --variant: %q", command.Argv)
	}
}

func TestOpenCodeV2CredentialConfig(t *testing.T) {
	home := t.TempDir()
	_, env, err := ApplyAgentCLICredential(AgentCLIOpenCode,
		ProviderCredential{APIKey: "secret", BaseURL: "https://gw.example/", Protocol: "chat"},
		AgentCLIOptions{Model: "glm-5"}, HostAgentCLIHome(home), home, nil)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(env["OPENCODE_CONFIG_CONTENT"]), &doc); err != nil {
		t.Fatalf("config: %v\n%s", err, env["OPENCODE_CONFIG_CONTENT"])
	}
	provider := doc["providers"].(map[string]any)["agentflow"].(map[string]any)
	if provider["package"] != "@opencode/ai/providers/openai-compatible" {
		t.Fatalf("provider package: %v", provider["package"])
	}
	settings := provider["settings"].(map[string]any)
	if settings["baseURL"] != "https://gw.example" || settings["apiKey"] != "{env:"+agentCLIKeyEnv+"}" {
		t.Fatalf("provider settings: %v", settings)
	}
	model := provider["models"].(map[string]any)["glm-5"].(map[string]any)
	variants, _ := model["variants"].([]any)
	var ids []string
	for _, raw := range variants {
		ids = append(ids, raw.(map[string]any)["id"].(string))
	}
	if !strings.Contains(strings.Join(ids, ","), "high") || !strings.Contains(strings.Join(ids, ","), "low") {
		t.Fatalf("effort variants: %v", ids)
	}
	if doc["model"] != "agentflow/glm-5" || doc["update"] != "disable" || doc["share"] != "disabled" {
		t.Fatalf("top-level config: %v", doc)
	}
	if env["OPENCODE_DISABLE_PROJECT_CONFIG"] != "1" || env["OPENCODE_DISABLE_CLAUDE_CODE"] != "1" {
		t.Fatalf("isolation env: %v", env)
	}
	if strings.Contains(env["OPENCODE_CONFIG_CONTENT"], "secret") {
		t.Fatal("the key must stay in the environment, never in config")
	}
}

func TestOpenCodeV2ProtocolPackages(t *testing.T) {
	for protocol, want := range map[string]string{
		"chat":      "@opencode/ai/providers/openai-compatible",
		"responses": "@opencode/ai/providers/openai-compatible/responses",
		"messages":  "@opencode/ai/providers/anthropic-compatible",
	} {
		if got := openCodeProtocolPackage(protocol); got != want {
			t.Fatalf("%s package = %q, want %q", protocol, got, want)
		}
	}
}

func TestOpenCodeV2StreamTranslator(t *testing.T) {
	translate := NewAgentCLITranslator(AgentCLIOpenCode)
	var lines []string
	for _, raw := range []string{
		`{"type":"text","timestamp":1,"sessionID":"ses_1","part":{"id":"prt_1","sessionID":"ses_1","messageID":"msg_1","type":"text","text":"hello","time":{"start":1,"end":2}}}`,
		`{"type":"tool_use","timestamp":2,"sessionID":"ses_1","part":{"partID":"prt_2","sessionID":"ses_1","messageID":"msg_1","type":"tool","id":"call_1","tool":"bash","state":{"status":"completed","input":{"command":"ls"},"output":"ok"}}}`,
		`{"type":"step_finish","timestamp":3,"sessionID":"ses_1","part":{"id":"prt_3","sessionID":"ses_1","messageID":"msg_1","type":"step-finish","reason":"stop","tokens":{"input":10,"output":5,"cache":{"read":1,"write":2},"reasoning":3}}}`,
	} {
		lines = append(lines, translate(raw)...)
	}
	joined := strings.Join(lines, "\n")
	for _, want := range []string{
		`"session_id":"ses_1"`,
		`"text":"hello"`,
		`"id":"call_1"`,
		`"tool_use_id":"call_1"`,
		`"type":"result"`,
		`"input_tokens":10`,
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("translated stream misses %q:\n%s", want, joined)
		}
	}
}

func TestOpenCodeMCPToolNameCanonicalized(t *testing.T) {
	for in, want := range map[string]string{
		"agentflow_session_submit_assistant_reply":     "mcp__agentflow_session__submit_assistant_reply",
		"agentflow_session_tapd__stories_get":          "mcp__agentflow_session__tapd__stories_get",
		"mcp__agentflow_session__submit_clarification": "mcp__agentflow_session__submit_clarification",
		"bash": "bash",
	} {
		if got := openCodeToolName(in); got != want {
			t.Fatalf("openCodeToolName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestOpenCodeV2ApprovalPluginUsesV2Hook(t *testing.T) {
	for _, want := range []string{
		"export default",
		`ctx.tool.hook("execute.before"`,
		"await approve(event.tool, event.input)",
	} {
		if !strings.Contains(conversationOpenCodeApproval, want) {
			t.Fatalf("v2 approval plugin misses %q:\n%s", want, conversationOpenCodeApproval)
		}
	}
}
