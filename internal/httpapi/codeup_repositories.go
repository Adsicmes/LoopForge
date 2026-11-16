package httpapi

import (
	"context"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"agentflow/internal/codeup"
	"agentflow/internal/domain"
	"agentflow/internal/gitwork"

	"github.com/gin-gonic/gin"
)

// codeupRepositories lists the Codeup repositories the caller's personal access
// token can see, so the repository picker offers them instead of asking people
// to paste paths. Yunxiao scopes every repository call by organization, so the
// organization id is required; the picker takes it from whatever Codeup address
// the user already typed, or from an explicit input.
func (h *usersHandlers) codeupRepositories(c *gin.Context) {
	user, ok := h.requestUser(c.Request, false)
	if !ok || user == nil || user.ID == 0 || user.IsGuest {
		c.JSON(http.StatusForbidden, gin.H{"error": "请先登录后再浏览云效仓库"})
		return
	}
	if h.repo == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "存储不可用"})
		return
	}
	organizationID := strings.TrimSpace(c.Query("org"))
	if organizationID == "" {
		// A pasted repository address carries the organization id too; accepting
		// it keeps the picker working before the user fills the org field.
		if info, parsed := domain.ParseCodeupRepoURL(c.Query("repo")); parsed {
			organizationID = info.OrganizationID
		}
	}
	if organizationID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少云效组织 ID：请先粘贴一个 Codeup 仓库地址，或填写组织 ID"})
		return
	}
	if !domain.IsCodeupOrganizationID(organizationID) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "云效组织 ID 格式不正确（应为 24 位十六进制字符）"})
		return
	}
	page, err := strconv.Atoi(c.DefaultQuery("page", "1"))
	if err != nil || page < 1 || page > 10000 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效页码"})
		return
	}
	_, token := gitwork.SplitToken(h.repo.GitCredential(c.Request.Context(), user.ID))
	if strings.TrimSpace(token) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "尚未配置云效个人访问令牌，请先在个人配置里填写 Git Token"})
		return
	}
	client, err := codeup.New(codeup.Config{
		APIBase:        strings.TrimSpace(os.Getenv(codeupAPIBaseEnv)),
		OrganizationID: organizationID,
		Token:          token,
	})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	repositories, total, err := client.ListRepositories(ctx, strings.TrimSpace(c.Query("search")), page, 100)
	if err != nil {
		status := http.StatusBadGateway
		if codeup.IsAuth(err) {
			status = http.StatusUnauthorized
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	items := make([]gin.H, 0, len(repositories))
	for _, repository := range repositories {
		info, ok := domain.ParseCodeupRepoURL(repository.PathWithNamespace)
		if !ok {
			continue
		}
		name := strings.TrimSpace(repository.Name)
		if name == "" {
			name = info.Name
		}
		webURL := strings.TrimSpace(repository.WebURL)
		if webURL == "" {
			webURL = info.WebURL
		}
		cloneURL := strings.TrimSpace(repository.HTTPURL)
		if cloneURL == "" {
			cloneURL = info.CloneURL
		}
		items = append(items, gin.H{
			"project":       info.Project,
			"name":          name,
			"webUrl":        webURL,
			"cloneUrl":      cloneURL,
			"defaultBranch": strings.TrimSpace(repository.DefaultBranch),
			"visibility":    repository.Visibility,
		})
	}
	c.JSON(http.StatusOK, gin.H{
		"repositories": items,
		"organization": organizationID,
		"total":        total,
		"page":         page,
		"hasMore":      page*100 < total,
	})
}
