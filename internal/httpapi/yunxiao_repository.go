package httpapi

import (
	"context"
	"net/http"
	"strings"
	"time"

	"agentflow/internal/domain"
	"agentflow/internal/gitwork"
	"agentflow/internal/yunxiao"

	"github.com/gin-gonic/gin"
)

// verifyYunxiaoRepository resolves a Codeup repository through OpenAPI and
// caches the numeric repository id, default branch and clone URLs. Saving a
// repository is allowed offline; delivery requires a successful verification.
func (h *catalogHandlers) verifyYunxiaoRepository(c *gin.Context) {
	repository, ok := h.repo.Repository(c.Param("id"))
	if !ok {
		c.JSON(http.StatusNotFound, map[string]string{"error": "repository not found"})
		return
	}
	if !h.canManageProjectRepositories(c, repository.ProjectID) {
		return
	}
	if repository.Provider != domain.RepositoryProviderYunxiao {
		c.JSON(http.StatusBadRequest, map[string]string{"error": "只有云效仓库可以执行该校验"})
		return
	}
	token := ""
	if current, ok := h.requestUser(c.Request, false); ok && current != nil {
		if user, found := h.repo.User(current.ID); found {
			token = strings.TrimSpace(user.YunxiaoToken)
		}
	}
	if token == "" {
		if project, ok := h.repo.Project(repository.ProjectID); ok {
			if owner, ok := h.repo.User(project.OwnerID); ok {
				token = strings.TrimSpace(owner.YunxiaoToken)
			}
		}
	}
	if token == "" {
		c.JSON(http.StatusBadRequest, map[string]string{"error": "请先在个人配置中填写云效访问令牌，或让项目 Owner 配置"})
		return
	}
	cfg := h.repo.SyncConfig()
	client := yunxiao.New(cfg.YunxiaoAPIBase, cfg.YunxiaoOrganizationID, token)
	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()
	remote := strings.TrimPrefix(strings.TrimSpace(repository.GongfengProjectID), "codeup:")
	if remote == "" {
		c.JSON(http.StatusBadRequest, map[string]string{"error": "云效仓库标识为空"})
		return
	}
	info, err := client.GetRepository(ctx, remote)
	if err != nil {
		c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if info.ID <= 0 {
		c.JSON(http.StatusBadRequest, map[string]string{"error": "云效没有返回有效的仓库 ID"})
		return
	}
	repository.YunxiaoRepoID = info.ID
	if strings.TrimSpace(repository.DefaultBranch) == "" {
		repository.DefaultBranch = strings.TrimSpace(info.DefaultBranch)
	}
	if strings.TrimSpace(repository.WebURL) == "" {
		repository.WebURL = strings.TrimSpace(info.WebURL)
	}
	if strings.TrimSpace(info.SSHURL) != "" {
		repository.CloneURL = strings.TrimSpace(info.SSHURL)
	} else if !gitwork.IsSSHReference(repository.CloneURL) {
		repository.CloneURL = domain.YunxiaoSSHCloneURL(repository.GongfengProjectID, repository.WebURL)
	}
	repository.UpdatedAt = time.Now().UTC()
	if err := h.repo.PutRepository(repository); err != nil {
		respond(c, err)
		return
	}
	stored, _ := h.repo.Repository(repository.ID)
	c.JSON(http.StatusOK, map[string]any{
		"valid":      true,
		"repository": domain.PublicRepository(stored),
		"access":     info.AccessLevel,
		"allowPush":  info.AllowPush,
	})
}
