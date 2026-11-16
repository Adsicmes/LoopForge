package httpapi

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"agentflow/internal/domain"

	"github.com/gin-gonic/gin"
)

// codeupWebhookSecret rotates the shared secret Codeup echoes back in the
// X-Codeup-Token header. The secret is returned exactly once: afterwards the
// repository payload only says whether one is configured, so a leaked API
// response cannot be replayed against the webhook.
func (h *catalogHandlers) codeupWebhookSecret(c *gin.Context) {
	repository, ok := h.repo.Repository(c.Param("id"))
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "repository not found"})
		return
	}
	if !h.canManageProjectRepositories(c, repository.ProjectID) {
		return
	}
	if c.Request.Method == http.MethodDelete {
		repository.WebhookSecret = ""
		if err := h.repo.PutRepository(repository); err != nil {
			respond(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "cleared"})
		return
	}
	if repository.Provider != domain.CodeupProvider {
		c.JSON(http.StatusBadRequest, gin.H{"error": "只有云效 Codeup 仓库支持 Webhook"})
		return
	}
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		respond(c, err)
		return
	}
	repository.WebhookSecret = hex.EncodeToString(buf)
	if err := h.repo.PutRepository(repository); err != nil {
		respond(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"secret": repository.WebhookSecret,
		"path":   "/api/webhooks/codeup/" + repository.ID,
		"header": "X-Codeup-Token",
	})
}

// codeupWebhook receives Codeup events for one repository and refreshes that
// repository's merge-request delivery states immediately.
//
// The payload is not parsed for event semantics on purpose: Codeup's event
// bodies are versioned by event type, and a delivery refresh only needs to know
// which repository moved. Anything the event names in passing is logged for
// troubleshooting, nothing else is trusted. Authentication is the shared secret
// only — the route is reachable without a session, because Codeup has no cookie.
func (h *catalogHandlers) codeupWebhook(c *gin.Context) {
	repository, ok := h.repo.Repository(c.Param("id"))
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "repository not found"})
		return
	}
	if strings.TrimSpace(repository.WebhookSecret) == "" {
		c.JSON(http.StatusForbidden, gin.H{"error": "该仓库还没有配置 Webhook 密钥"})
		return
	}
	// Compared byte for byte: HTTP strips surrounding whitespace from header
	// values, so any difference left is a real mismatch.
	presented := c.GetHeader("X-Codeup-Token")
	if subtle.ConstantTimeCompare([]byte(presented), []byte(repository.WebhookSecret)) != 1 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "X-Codeup-Token 不匹配"})
		return
	}
	body, _ := io.ReadAll(io.LimitReader(c.Request.Body, 1<<20))
	event := codeupEventSummary(body)
	if h.sync == nil {
		c.JSON(http.StatusOK, gin.H{"status": "ignored", "reason": "同步服务未配置"})
		return
	}
	// The response is sent before the refresh finishes: Codeup retries a webhook
	// that is slow or non-2xx, and a delivery refresh is idempotent anyway.
	go h.sync.RefreshCodeupRepositoryDeliveries(h.repo, repository.ID)
	c.JSON(http.StatusOK, gin.H{"status": "accepted", "repository": repository.ID, "event": event})
}

// codeupEventSummary extracts a human-readable event name for logs. It never
// feeds control flow.
func codeupEventSummary(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	var payload struct {
		ObjectKind string `json:"object_kind"`
		EventName  string `json:"event_name"`
		ObjectType string `json:"objectType"`
		EventType  string `json:"eventType"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return ""
	}
	for _, candidate := range []string{payload.EventName, payload.ObjectKind, payload.EventType, payload.ObjectType} {
		if value := strings.TrimSpace(candidate); value != "" {
			return value
		}
	}
	return ""
}
