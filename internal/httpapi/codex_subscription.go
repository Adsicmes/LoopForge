package httpapi

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"agentflow/internal/domain"

	"github.com/gin-gonic/gin"
)

// codexSubscriptionHandlers owns personal ChatGPT subscription credentials
// and the platform-level switches. It is intentionally separate from
// usersHandlers so credentials and platform settings do not grow new methods
// on unrelated domains.
type codexSubscriptionHandlers struct {
	*deps
	mu     sync.Mutex
	flows  map[int64]*codexDeviceFlow
	byID   map[string]*codexDeviceFlow
	client *http.Client
}

type codexDeviceFlow struct {
	id              string
	userID          int64
	userCode        string
	verificationURL string
	expires         time.Time
	status          string // pending, connected, error
	err             string
	wantCode        bool
	cancel          context.CancelFunc
	ready           chan struct{}
	readyOnce       sync.Once
}

var codexLoginANSI = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)
var codexLoginURL = regexp.MustCompile(`https?://[^\s\x1b]+/codex/device`)
var codexLoginCode = regexp.MustCompile(`\b[A-Z0-9]{4,8}-[A-Z0-9]{4,8}\b`)

func newCodexSubscriptionHandlers(d *deps) *codexSubscriptionHandlers {
	return &codexSubscriptionHandlers{
		deps:  d,
		flows: map[int64]*codexDeviceFlow{},
		byID:  map[string]*codexDeviceFlow{},
		client: &http.Client{Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          64,
			IdleConnTimeout:       90 * time.Second,
			ResponseHeaderTimeout: 5 * time.Minute,
		}},
	}
}

func (h *codexSubscriptionHandlers) current(c *gin.Context) (domain.User, bool) {
	c.Header("Cache-Control", "no-store")
	actor, ok := h.requestUser(c.Request, false)
	if !ok || actor == nil || actor.ID <= 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "请先登录 LoopForge"})
		return domain.User{}, false
	}
	id, err := parsePositiveInt64(c.Param("id"))
	if err != nil || id != actor.ID {
		c.JSON(http.StatusForbidden, gin.H{"error": "只能管理当前登录用户的 Codex 订阅"})
		return domain.User{}, false
	}
	user, ok := h.repo.User(actor.ID)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "请先登录 LoopForge"})
		return domain.User{}, false
	}
	return user, true
}

func parsePositiveInt64(raw string) (int64, error) {
	id, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("invalid id")
	}
	return id, nil
}

func (h *codexSubscriptionHandlers) adminWrite(c *gin.Context) bool {
	if !h.isPlatformAdmin(c.Request) {
		c.JSON(http.StatusForbidden, gin.H{"error": "只有平台管理员可以配置 Codex 订阅"})
		return false
	}
	if _, bearer := bearerToken(c.Request); bearer {
		return true
	}
	origin, err := url.Parse(c.GetHeader("Origin"))
	if err != nil || origin.Host != c.Request.Host || (origin.Scheme != "http" && origin.Scheme != "https") {
		c.JSON(http.StatusForbidden, gin.H{"error": "请求来源不允许"})
		return false
	}
	return true
}

func (h *codexSubscriptionHandlers) status(c *gin.Context) {
	user, ok := h.current(c)
	if !ok {
		return
	}
	result := gin.H{
		"enabled":                 h.repo.CodexSubscriptionEnabled(),
		"mode":                    user.CodexAuthMode,
		"configured":              user.CodexSubscriptionConfigured,
		"needsReauth":             user.CodexSubscriptionNeedsReauth,
		"authJsonAllowed":         h.repo.CodexSubscriptionAuthJSONEnabled(),
		"credentialKeyConfigured": domain.CredentialKeyConfigured(),
	}
	if status, found := h.repo.CodexSubscriptionStatus(user.ID); found {
		result["configured"] = true
		result["needsReauth"] = status.NeedsReauth
		result["email"] = status.Email
		result["plan"] = status.PlanType
		result["connectedAt"] = status.ConnectedAt
		result["lastRefreshAt"] = status.LastRefreshAt
		result["lastError"] = status.LastError
	}
	c.JSON(http.StatusOK, result)
}

func (h *codexSubscriptionHandlers) platformSettings(c *gin.Context) {
	if !h.isPlatformAdmin(c.Request) {
		c.JSON(http.StatusForbidden, gin.H{"error": "只有平台管理员可以查看 Codex 订阅设置"})
		return
	}
	if c.Request.Method == http.MethodGet {
		c.JSON(http.StatusOK, h.platformSettingsPayload())
		return
	}
	if !h.adminWrite(c) {
		return
	}
	var body struct {
		Enabled         *bool `json:"enabled"`
		AuthJSONAllowed *bool `json:"authJsonAllowed"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 4096)).Decode(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的设置请求"})
		return
	}
	if body.Enabled != nil && *body.Enabled && !domain.CredentialKeyConfigured() {
		c.JSON(http.StatusBadRequest, gin.H{"error": "启用 Codex 订阅前请先配置 AGENTFLOW_CREDENTIAL_KEY"})
		return
	}
	if body.Enabled != nil {
		if err := h.repo.SetCodexSubscriptionEnabled(*body.Enabled); err != nil {
			respond(c, err)
			return
		}
	}
	if body.AuthJSONAllowed != nil {
		if err := h.repo.SetCodexSubscriptionAuthJSONEnabled(*body.AuthJSONAllowed); err != nil {
			respond(c, err)
			return
		}
	}
	c.JSON(http.StatusOK, h.platformSettingsPayload())
}

func (h *codexSubscriptionHandlers) platformSettingsPayload() gin.H {
	return gin.H{
		"enabled":                 h.repo.CodexSubscriptionEnabled(),
		"authJsonAllowed":         h.repo.CodexSubscriptionAuthJSONEnabled(),
		"connectedUsers":          h.repo.CountCodexSubscriptions(),
		"credentialKeyConfigured": domain.CredentialKeyConfigured(),
	}
}

func (h *codexSubscriptionHandlers) startDevice(c *gin.Context) {
	user, ok := h.current(c)
	if !ok {
		return
	}
	if !h.repo.CodexSubscriptionEnabled() {
		c.JSON(http.StatusForbidden, gin.H{"error": "Codex 订阅能力未开启，请联系平台管理员"})
		return
	}
	if !domain.CredentialKeyConfigured() {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "服务端未配置 AGENTFLOW_CREDENTIAL_KEY，暂时不能保存订阅"})
		return
	}
	bin, err := resolveAssistantEngineBin(assistantEngineCodex)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}

	h.mu.Lock()
	if existing := h.flows[user.ID]; existing != nil && existing.status == "pending" && time.Now().Before(existing.expires) {
		h.mu.Unlock()
		c.JSON(http.StatusConflict, gin.H{"error": "已有一个正在进行的设备码登录，请先完成或取消"})
		return
	}
	if existing := h.flows[user.ID]; existing != nil {
		if existing.cancel != nil {
			existing.cancel()
		}
		delete(h.byID, existing.id)
		delete(h.flows, user.ID)
	}
	flow := &codexDeviceFlow{
		id:      randomHex(16),
		userID:  user.ID,
		expires: time.Now().UTC().Add(15 * time.Minute),
		status:  "pending",
		ready:   make(chan struct{}),
	}
	h.flows[user.ID] = flow
	h.byID[flow.id] = flow
	h.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	flow.cancel = cancel
	go h.runDeviceLogin(ctx, flow, bin)

	select {
	case <-flow.ready:
	case <-time.After(5 * time.Second):
	}
	h.mu.Lock()
	payload := h.flowPayloadLocked(flow)
	h.mu.Unlock()
	if flow.status == "error" {
		c.JSON(http.StatusBadGateway, gin.H{"error": flow.err, "flowId": flow.id})
		return
	}
	c.JSON(http.StatusOK, payload)
}

func (h *codexSubscriptionHandlers) pollDevice(c *gin.Context) {
	user, ok := h.current(c)
	if !ok {
		return
	}
	var body struct {
		FlowID string `json:"flowId"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 4096)).Decode(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的轮询请求"})
		return
	}
	h.mu.Lock()
	flow := h.byID[strings.TrimSpace(body.FlowID)]
	if flow == nil || flow.userID != user.ID {
		h.mu.Unlock()
		c.JSON(http.StatusNotFound, gin.H{"error": "登录流程不存在或已结束"})
		return
	}
	payload := h.flowPayloadLocked(flow)
	terminal := flow.status != "pending"
	if terminal {
		delete(h.byID, flow.id)
		delete(h.flows, flow.userID)
		if flow.cancel != nil {
			flow.cancel()
		}
	}
	h.mu.Unlock()
	c.JSON(http.StatusOK, payload)
}

func (h *codexSubscriptionHandlers) flowPayloadLocked(flow *codexDeviceFlow) gin.H {
	payload := gin.H{
		"flowId":          flow.id,
		"status":          flow.status,
		"userCode":        flow.userCode,
		"verificationUrl": flow.verificationURL,
		"expiresAt":       flow.expires,
	}
	if flow.err != "" {
		payload["error"] = flow.err
	}
	return payload
}

func (h *codexSubscriptionHandlers) runDeviceLogin(ctx context.Context, flow *codexDeviceFlow, bin string) {
	dir, err := os.MkdirTemp("", "loopforge-codex-login-")
	if err != nil {
		h.finishFlow(flow, "error", "创建登录临时目录失败")
		return
	}
	defer os.RemoveAll(dir)
	home := filepath.Join(dir, "home")
	codexHome := filepath.Join(home, ".codex")
	if err := os.MkdirAll(codexHome, 0o700); err != nil {
		h.finishFlow(flow, "error", "创建 Codex 登录目录失败")
		return
	}
	if err := os.WriteFile(filepath.Join(codexHome, "config.toml"), []byte("cli_auth_credentials_store = \"file\"\n"), 0o600); err != nil {
		h.finishFlow(flow, "error", "写入 Codex 登录配置失败")
		return
	}

	cmd := exec.CommandContext(ctx, bin, "login", "--device-auth")
	cmd.Dir = home
	cmd.Env = codexLoginEnvironment(home, codexHome)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		h.finishFlow(flow, "error", "启动 Codex 登录失败")
		return
	}
	stderr := &limitedBuffer{limit: 8 << 10}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		h.finishFlow(flow, "error", "启动 Codex 登录失败："+sanitizeLoginError(err.Error()))
		return
	}

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64<<10), 64<<10)
	for scanner.Scan() {
		h.consumeLoginLine(flow, scanner.Text())
	}
	waitErr := cmd.Wait()
	if waitErr != nil {
		message := sanitizeLoginError(stderr.String())
		if message == "" {
			message = "Codex 登录失败"
		}
		if strings.Contains(strings.ToLower(message), "device code login is not enabled") {
			message = "该账号或工作区未开启设备码登录，请到 ChatGPT 安全设置/工作区权限开启，或让管理员开启 auth.json 兜底"
		}
		h.finishFlow(flow, "error", message)
		return
	}
	authJSON, err := os.ReadFile(filepath.Join(codexHome, "auth.json"))
	if err != nil {
		h.finishFlow(flow, "error", "Codex 登录完成但没有生成 auth.json")
		return
	}
	if _, err := domain.ParseCodexAuthJSON(authJSON); err != nil {
		h.finishFlow(flow, "error", err.Error())
		return
	}
	if _, err := h.repo.SaveCodexSubscriptionAndMode(flow.userID, authJSON); err != nil {
		h.finishFlow(flow, "error", "保存 Codex 订阅失败："+sanitizeLoginError(err.Error()))
		return
	}
	h.finishFlow(flow, "connected", "")
}

func (h *codexSubscriptionHandlers) consumeLoginLine(flow *codexDeviceFlow, line string) {
	line = strings.TrimSpace(codexLoginANSI.ReplaceAllString(line, ""))
	if line == "" {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if match := codexLoginURL.FindString(line); match != "" {
		flow.verificationURL = match
	}
	if strings.Contains(strings.ToLower(line), "one-time code") {
		flow.wantCode = true
	}
	if flow.userCode == "" {
		if match := codexLoginCode.FindString(line); match != "" {
			flow.userCode = match
			flow.wantCode = false
		} else if flow.wantCode {
			code := strings.TrimSpace(line)
			if fields := strings.Fields(code); len(fields) > 0 {
				code = fields[0]
			}
			if code != "" && strings.Contains(code, "-") && !strings.Contains(strings.ToLower(code), "one-time") && !strings.HasPrefix(code, "http") {
				flow.userCode = code
				flow.wantCode = false
			}
		}
	}
	if flow.userCode != "" && flow.verificationURL != "" {
		flow.readyOnce.Do(func() { close(flow.ready) })
	}
}

func (h *codexSubscriptionHandlers) finishFlow(flow *codexDeviceFlow, status, message string) {
	h.mu.Lock()
	flow.status = status
	flow.err = message
	h.mu.Unlock()
	flow.readyOnce.Do(func() { close(flow.ready) })
	if flow.cancel != nil {
		flow.cancel()
	}
}

func codexLoginEnvironment(home, codexHome string) []string {
	keep := []string{"PATH", "HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "ALL_PROXY", "SSL_CERT_FILE", "CODEX_CA_CERTIFICATE", "LANG", "LC_ALL", "TERM"}
	env := []string{"HOME=" + home, "TMPDIR=" + filepath.Join(home, "tmp"), "CODEX_HOME=" + codexHome}
	_ = os.MkdirAll(filepath.Join(home, "tmp"), 0o700)
	for _, key := range keep {
		if value, ok := os.LookupEnv(key); ok && strings.TrimSpace(value) != "" {
			env = append(env, key+"="+value)
		}
	}
	return env
}

func sanitizeLoginError(raw string) string {
	raw = strings.TrimSpace(codexLoginANSI.ReplaceAllString(raw, ""))
	raw = strings.ReplaceAll(raw, "\r", "\n")
	raw = diagnosticBearer.ReplaceAllString(raw, "$1 <redacted>")
	raw = diagnosticSecret.ReplaceAllString(raw, "${1}<redacted>")
	lines := strings.Split(raw, "\n")
	kept := make([]string, 0, 3)
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.Contains(line, "Warning:") {
			continue
		}
		kept = append(kept, line)
		if len(kept) == 3 {
			break
		}
	}
	message := strings.Join(kept, "；")
	if len(message) > 500 {
		message = message[:500]
	}
	return message
}

type limitedBuffer struct {
	buf   bytes.Buffer
	limit int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	remaining := b.limit - b.buf.Len()
	if remaining > 0 {
		if len(p) > remaining {
			_, _ = b.buf.Write(p[:remaining])
		} else {
			_, _ = b.buf.Write(p)
		}
	}
	return len(p), nil
}

func (b *limitedBuffer) String() string { return b.buf.String() }

func randomHex(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}

// pasteAuthJSON accepts a file generated by `codex login` on a machine where
// device code login is unavailable. The body is never echoed.
func (h *codexSubscriptionHandlers) pasteAuthJSON(c *gin.Context) {
	user, ok := h.current(c)
	if !ok {
		return
	}
	if !h.repo.CodexSubscriptionEnabled() || !h.repo.CodexSubscriptionAuthJSONEnabled() {
		c.JSON(http.StatusForbidden, gin.H{"error": "auth.json 兜底未开启"})
		return
	}
	if !domain.CredentialKeyConfigured() {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "服务端未配置 AGENTFLOW_CREDENTIAL_KEY，暂时不能保存订阅"})
		return
	}
	origin, err := url.Parse(c.GetHeader("Origin"))
	if err != nil || origin.Host != c.Request.Host || (origin.Scheme != "http" && origin.Scheme != "https") {
		c.JSON(http.StatusForbidden, gin.H{"error": "请求来源不允许"})
		return
	}
	var body struct {
		AuthJSON string `json:"authJson"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 96<<10)).Decode(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的 auth.json 请求"})
		return
	}
	raw := []byte(strings.TrimSpace(body.AuthJSON))
	if _, err := domain.ParseCodexAuthJSON(raw); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if _, err := h.repo.SaveCodexSubscriptionAndMode(user.ID, raw); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	h.status(c)
}

func (h *codexSubscriptionHandlers) disconnect(c *gin.Context) {
	user, ok := h.current(c)
	if !ok {
		return
	}
	h.mu.Lock()
	if flow := h.flows[user.ID]; flow != nil {
		if flow.cancel != nil {
			flow.cancel()
		}
		delete(h.byID, flow.id)
		delete(h.flows, user.ID)
	}
	h.mu.Unlock()
	if err := h.repo.DeleteCodexSubscriptionAndMode(user.ID); err != nil {
		respond(c, err)
		return
	}
	h.status(c)
}
