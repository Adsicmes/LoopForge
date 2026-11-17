package engine

import (
	"os"
	"path/filepath"

	"agentflow/internal/domain"
	"agentflow/internal/runtime"
)

// captureCodexSubscription reads a refreshed auth.json from the run's private
// HOME and writes it back under the credential generation that produced it.
// A stale write is discarded by the repo. On success the file is removed so a
// later script node cannot read the long-lived token; on failure it is kept
// for the next run's newest-wins import.
func (e *Engine) captureCodexSubscription(run domain.Run, sb *runEnvironment) {
	if e == nil || e.repo == nil || sb == nil {
		return
	}
	cred := sb.credentialSnapshot().Codex
	if cred.AuthMode != domain.CodexAuthSubscription || cred.AuthGeneration <= 0 {
		return
	}
	path := filepath.Join(sb.Workspace(), ".agentflow", "private-home", "codex", ".codex", "auth.json")
	raw, err := runtime.ReadCodexAuthFile(sb.Workspace(), path)
	if err != nil {
		return
	}
	ok, err := e.repo.CaptureCodexSubscription(run.ExecutionContext.ResponsibleUserID, cred.AuthGeneration, raw)
	if err != nil || !ok {
		return
	}
	_ = os.Remove(path)
}
