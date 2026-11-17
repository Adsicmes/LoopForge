package runtime

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"agentflow/internal/domain"
)

// WriteCodexAuthFile writes a subscription auth.json into a private HOME.
// When the existing file has the same or a newer last_refresh, it is kept:
// an earlier run may already have refreshed the rotating token.
func WriteCodexAuthFile(workspace, target string, authJSON []byte) error {
	if _, err := domain.ParseCodexAuthJSON(authJSON); err != nil {
		return err
	}
	if existing, err := readWorkspaceFile(workspace, target); err == nil {
		if !authFileNewer(authJSON, existing) {
			return nil
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return err
	}
	return writeManagedFile(workspace, target, authJSON, 0o600)
}

// ReadCodexAuthFile reads a runtime auth.json for capture. The workspace is
// the private HOME root, not necessarily the run workspace.
func ReadCodexAuthFile(workspace, target string) ([]byte, error) {
	return readWorkspaceFile(workspace, target)
}

func authFileNewer(next, existing []byte) bool {
	var a, b struct {
		LastRefresh *time.Time `json:"last_refresh"`
	}
	if json.Unmarshal(existing, &b) != nil || b.LastRefresh == nil {
		return true
	}
	if json.Unmarshal(next, &a) != nil || a.LastRefresh == nil {
		return false
	}
	return a.LastRefresh.After(*b.LastRefresh)
}

// codexAuthFileTarget returns the host path and the root that writeManagedFile
// should anchor on. PrivateHome commands live under the run workspace; chat
// commands use their durable private HOME.
func codexAuthFileTarget(command CommandSpec, bound CommandSpec, workspace string) (path, root string, err error) {
	if command.PrivateHome {
		if strings.TrimSpace(workspace) == "" {
			return "", "", fmt.Errorf("Codex 私有 HOME 缺少工作区")
		}
		name := PrivateHomeName(command)
		return filepath.Join(workspace, ".agentflow", "private-home", name, ".codex", "auth.json"), workspace, nil
	}
	home := strings.TrimSpace(bound.environment["HOME"])
	if home == "" {
		return "", "", fmt.Errorf("Codex 对话缺少私有 HOME")
	}
	codexHome := strings.TrimSpace(bound.environment["CODEX_HOME"])
	if codexHome == "" {
		codexHome = filepath.Join(home, ".codex")
	}
	return filepath.Join(codexHome, "auth.json"), home, nil
}

// insertBeforeLast inserts config overrides before the final argv element
// (the prompt or --stdio). It mirrors CodexUserProviderArgv ordering.
func insertBeforeLast(argv []string, extra ...string) []string {
	if len(argv) == 0 {
		return append([]string(nil), extra...)
	}
	out := make([]string, 0, len(argv)+len(extra))
	out = append(out, argv[:len(argv)-1]...)
	out = append(out, extra...)
	return append(out, argv[len(argv)-1])
}

// codexGatewayProviderArgs points Codex at the platform gateway. Codex sends
// AGENTFLOW_CODEX_GATEWAY_TOKEN as its bearer; the gateway replaces it with
// the real OAuth token and never forwards the run token upstream.
func codexGatewayProviderArgs(baseURL string) []string {
	return []string{
		"-c", `model_provider="agentflow_gateway"`,
		"-c", `model_providers.agentflow_gateway.name="LoopForge Gateway"`,
		"-c", "model_providers.agentflow_gateway.base_url=" + strconv.Quote(strings.TrimSpace(baseURL)),
		"-c", `model_providers.agentflow_gateway.env_key="AGENTFLOW_CODEX_GATEWAY_TOKEN"`,
		"-c", `model_providers.agentflow_gateway.wire_api="responses"`,
		"-c", `model_providers.agentflow_gateway.requires_openai_auth=false`,
	}
}

// CodexGatewayProviderArgv is the runner-side counterpart of BindCodex's
// gateway branch: it inserts the custom provider before the prompt.
func CodexGatewayProviderArgv(argv []string, baseURL string) ([]string, error) {
	if len(argv) < 2 || strings.TrimSpace(baseURL) == "" {
		return nil, fmt.Errorf("invalid Codex gateway invocation")
	}
	return insertBeforeLast(argv, codexGatewayProviderArgs(baseURL)...), nil
}
