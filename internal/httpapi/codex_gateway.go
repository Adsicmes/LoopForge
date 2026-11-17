package httpapi

import (
	"errors"
	"net/http"
	"net/url"
	"os"
	"strings"

	"agentflow/internal/codexauth"
	"agentflow/internal/domain"

	"github.com/gin-gonic/gin"
)

const defaultCodexUpstream = "https://chatgpt.com/backend-api/codex"

var codexGatewayHopHeaders = map[string]bool{
	"connection":          true,
	"keep-alive":          true,
	"proxy-authenticate":  true,
	"proxy-authorization": true,
	"te":                  true,
	"trailer":             true,
	"transfer-encoding":   true,
	"upgrade":             true,
	"host":                true,
	"authorization":       true,
}

// gateway is the V2 credential broker. Codex on a runner points its custom
// provider at this endpoint with a short-lived run token; the gateway replaces
// that token with the user's OAuth access token and never exposes the refresh
// token to the runner.
func (h *codexSubscriptionHandlers) gateway(c *gin.Context) {
	raw := strings.TrimSpace(c.GetHeader("Authorization"))
	token := strings.TrimSpace(strings.TrimPrefix(raw, "Bearer "))
	if token == "" || token == raw {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Codex gateway token required"})
		return
	}
	claims, err := codexauth.Verify(token)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Codex gateway token invalid or expired"})
		return
	}
	if !h.repo.CodexSubscriptionEnabled() {
		c.JSON(http.StatusForbidden, gin.H{"error": "Codex 订阅能力未开启"})
		return
	}
	if !domain.CredentialKeyConfigured() {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "服务端未配置 AGENTFLOW_CREDENTIAL_KEY"})
		return
	}
	if rid := strings.TrimSpace(claims.RunID); rid != "" {
		run, ok := h.repo.RunState(rid)
		if !ok || (run.Status != "running" && run.Status != "waiting") {
			c.JSON(http.StatusForbidden, gin.H{"error": "Codex 运行已结束"})
			return
		}
	}
	if status, ok := h.repo.CodexSubscriptionStatus(claims.UserID); !ok || status.NeedsReauth {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Codex 订阅未连接或需要重新登录"})
		return
	}

	path := c.Param("path")
	if path == "" {
		path = "/"
	}
	switch {
	case c.Request.Method == http.MethodPost && path == "/responses":
	case c.Request.Method == http.MethodGet && path == "/models":
	default:
		c.JSON(http.StatusNotFound, gin.H{"error": "unsupported Codex gateway path"})
		return
	}

	authJSON, accessToken, accountID, err := codexauth.AccessToken(c.Request.Context(), h.repo, claims.UserID)
	if err != nil {
		var reauth *codexauth.ReauthError
		switch {
		case errors.As(err, &reauth), errors.Is(err, codexauth.ErrNoCredential):
			c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		case errors.Is(err, codexauth.ErrRefreshBusy):
			c.Header("Retry-After", "1")
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Codex 订阅正在刷新，请稍后重试"})
		default:
			c.JSON(http.StatusBadGateway, gin.H{"error": "Codex 订阅刷新失败"})
		}
		_ = authJSON
		return
	}

	upstream := strings.TrimSpace(os.Getenv("AGENTFLOW_CODEX_UPSTREAM"))
	if upstream == "" {
		upstream = defaultCodexUpstream
	}
	target, err := url.Parse(strings.TrimRight(upstream, "/") + path)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "Codex upstream is invalid"})
		return
	}
	target.RawQuery = c.Request.URL.RawQuery

	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64<<20)
	req, err := http.NewRequestWithContext(c.Request.Context(), c.Request.Method, target.String(), c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid Codex gateway request"})
		return
	}
	req.ContentLength = c.Request.ContentLength
	copyGatewayHeaders(req.Header, c.Request.Header)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	if strings.TrimSpace(accountID) != "" {
		req.Header.Set("chatgpt-account-id", accountID)
	}

	resp, err := h.client.Do(req)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "Codex upstream unavailable"})
		return
	}
	defer resp.Body.Close()
	for key, values := range resp.Header {
		if codexGatewayHopHeaders[strings.ToLower(key)] {
			continue
		}
		for _, value := range values {
			c.Writer.Header().Add(key, value)
		}
	}
	c.Writer.Header().Set("Cache-Control", "no-store")
	c.Writer.WriteHeader(resp.StatusCode)
	flusher, _ := c.Writer.(http.Flusher)
	if flusher != nil {
		flusher.Flush()
	}
	buf := make([]byte, 32<<10)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, writeErr := c.Writer.Write(buf[:n]); writeErr != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		if readErr != nil {
			return
		}
	}
}

func copyGatewayHeaders(dst, src http.Header) {
	for key, values := range src {
		if codexGatewayHopHeaders[strings.ToLower(key)] {
			continue
		}
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}
