package engine

import (
	"context"
	"errors"
	"sync"
	"time"

	"agentflow/internal/domain"
	"agentflow/internal/runtime"
)

// codexRunLeaseTTL is refreshed while a platform-hosted subscription node
// runs. If the API process dies, the lease frees itself after this window.
const codexRunLeaseTTL = 10 * time.Minute

// acquireCodexRunLease serializes platform-hosted Codex subscription work per
// user. Each run copies auth.json into its own private HOME, so two nodes
// refreshing the same rotating refresh token at once would invalidate one of
// them. Execution-machine runs skip this lease: only the control-plane gateway
// refreshes there, under its own per-user lease.
func (e *Engine) acquireCodexRunLease(ctx context.Context, userID int64, owner string, cred runtime.ProviderCredential, sink runtime.EventSink) (func(), error) {
	if e == nil || e.repo == nil || userID <= 0 || cred.AuthMode != domain.CodexAuthSubscription {
		return func() {}, nil
	}
	if owner == "" {
		owner = "codex-subscription"
	}
	waited := false
	for {
		ok, err := e.repo.AcquireCodexRunLease(userID, owner, codexRunLeaseTTL)
		if err != nil {
			return nil, err
		}
		if ok {
			break
		}
		if _, found := e.repo.CodexSubscriptionStatus(userID); !found {
			return nil, errors.New("Codex 订阅已断开，请在个人配置中重新连接")
		}
		if !waited {
			waited = true
			if sink != nil {
				sink(domain.TelemetryEvent{
					Category: "codex", Name: "codex.subscription_lease_wait", Phase: "start", Level: "warn",
					Message: "同一账号的 Codex 订阅正在另一个任务中运行，本节点排队等待",
				})
			}
		}
		timer := time.NewTimer(2 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(codexRunLeaseTTL / 3)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				// Renewing with the same owner extends the deadline. A failed
				// renewal is retried on the next tick; the node keeps running.
				_, _ = e.repo.AcquireCodexRunLease(userID, owner, codexRunLeaseTTL)
			}
		}
	}()
	var once sync.Once
	return func() {
		once.Do(func() {
			close(stop)
			<-done
			_ = e.repo.ReleaseCodexRunLease(userID, owner)
		})
	}, nil
}
