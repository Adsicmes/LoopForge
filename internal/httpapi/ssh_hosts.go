package httpapi

import (
	"net/http"
	"net/url"
	"strconv"

	"agentflow/internal/gitwork"

	"github.com/gin-gonic/gin"
)

type sshHostsHandlers struct{ *deps }

func (h *sshHostsHandlers) authorize(c *gin.Context, write bool) bool {
	if !h.isPlatformAdmin(c.Request) {
		c.JSON(http.StatusForbidden, gin.H{"error": "只有平台管理员可以管理 SSH 可信主机"})
		return false
	}
	if write {
		if _, ok := bearerToken(c.Request); !ok {
			origin, err := url.Parse(c.GetHeader("Origin"))
			if err != nil || origin.Host != c.Request.Host || (origin.Scheme != "http" && origin.Scheme != "https") {
				c.JSON(http.StatusForbidden, gin.H{"error": "invalid request origin"})
				return false
			}
		}
	}
	return true
}

func (h *sshHostsHandlers) list(c *gin.Context) {
	if !h.authorize(c, false) {
		return
	}
	items, err := h.repo.SSHHosts()
	if err != nil {
		c.JSON(500, gin.H{"error": "读取可信主机失败"})
		return
	}
	// github.com and ssh.github.com:443 are trusted by the platform's builtin
	// pins (upstream 48cdf0c A2) even without a stored entry; the UI shows the
	// note instead of asking the administrator to add them.
	c.JSON(200, gin.H{"hosts": items, "builtin": gitwork.BuiltinTrustEndpoints()})
}

func (h *sshHostsHandlers) preview(c *gin.Context) {
	if !h.authorize(c, true) {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 2<<20)
	var input struct {
		Content string `json:"content"`
	}
	if c.ShouldBindJSON(&input) != nil {
		c.JSON(400, gin.H{"error": "请求无效"})
		return
	}
	items, err := gitwork.ParseKnownHostsImport([]byte(input.Content))
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"items": items})
}

func (h *sshHostsHandlers) save(c *gin.Context) {
	if !h.authorize(c, true) {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 2<<20)
	var input struct {
		Hosts []gitwork.KnownHostCandidate `json:"hosts"`
	}
	if c.ShouldBindJSON(&input) != nil {
		c.JSON(400, gin.H{"error": "请求无效"})
		return
	}
	if err := h.repo.PutSSHHosts(input.Hosts); err != nil {
		c.JSON(400, gin.H{"error": "保存失败，请检查主机公钥"})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

func (h *sshHostsHandlers) remove(c *gin.Context) {
	if !h.authorize(c, true) {
		return
	}
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		c.JSON(400, gin.H{"error": "无效 ID"})
		return
	}
	if h.repo.DeleteSSHHost(id) != nil {
		c.JSON(500, gin.H{"error": "删除失败"})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

func (h *sshHostsHandlers) scan(c *gin.Context) {
	if !h.authorize(c, true) {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
	var input struct {
		Host string `json:"host"`
		Port int    `json:"port"`
	}
	if c.ShouldBindJSON(&input) != nil {
		c.JSON(400, gin.H{"error": "请求无效"})
		return
	}
	items, err := gitwork.ScanSSHHost(c.Request.Context(), input.Host, input.Port)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"hosts": items, "warning": "候选来自平台扫描，未经验证；请通过独立可信渠道核对。部分算法或密钥可能未发现"})
}
