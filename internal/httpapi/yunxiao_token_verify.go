package httpapi

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"agentflow/internal/domain"
	"agentflow/internal/yunxiao"

	"github.com/gin-gonic/gin"
)

type yunxiaoTokenVerifyResult struct {
	Valid         bool     `json:"valid"`
	Organizations []string `json:"organizations,omitempty"`
	Error         string   `json:"error,omitempty"`
}

// verifyYunxiaoToken verifies the current user's Yunxiao personal access
// token. GET checks the stored token; POST may carry an unsaved token in
// body.token so the settings form can verify before saving.
func (h *usersHandlers) verifyYunxiaoToken(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, yunxiaoTokenVerifyResult{Error: "valid user id is required"})
		return
	}
	current, ok := h.requestUser(c.Request, false)
	if !ok || current.ID != id {
		c.JSON(http.StatusForbidden, yunxiaoTokenVerifyResult{Error: "只能校验当前登录用户的凭据"})
		return
	}
	token := ""
	if c.Request.Method == http.MethodPost {
		var req struct {
			Token string `json:"token"`
		}
		if !decode(c, &req) {
			return
		}
		token = strings.TrimSpace(req.Token)
	}
	if token == "" {
		if h.repo == nil {
			c.JSON(http.StatusOK, yunxiaoTokenVerifyResult{Error: "存储不可用"})
			return
		}
		user, ok := h.repo.User(id)
		if !ok {
			c.JSON(http.StatusOK, yunxiaoTokenVerifyResult{Error: "用户不存在"})
			return
		}
		token = strings.TrimSpace(user.YunxiaoToken)
	}
	if token == "" {
		c.JSON(http.StatusOK, yunxiaoTokenVerifyResult{Error: "尚未配置云效访问令牌，请先填写 PAT"})
		return
	}
	cfg := h.repo.SyncConfig()
	repositoryPath := ""
	for _, repository := range h.repo.Repositories() {
		if repository.Provider == domain.RepositoryProviderYunxiao {
			repositoryPath = strings.TrimPrefix(strings.TrimSpace(repository.GongfengProjectID), "codeup:")
			break
		}
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	c.JSON(http.StatusOK, verifyYunxiaoPAT(ctx, cfg, token, repositoryPath))
}

func verifyYunxiaoPAT(ctx context.Context, cfg domain.SyncConfig, token, repositoryPath string) yunxiaoTokenVerifyResult {
	client, err := yunxiao.NewChecked(cfg.YunxiaoAPIBase, cfg.YunxiaoOrganizationID, token)
	if err != nil {
		return yunxiaoTokenVerifyResult{Error: err.Error()}
	}
	orgs, err := client.ListOrganizations(ctx)
	if err == nil {
		names := make([]string, 0, len(orgs))
		for _, org := range orgs {
			if name := strings.TrimSpace(org.Name); name != "" {
				names = append(names, name)
			}
		}
		return yunxiaoTokenVerifyResult{Valid: true, Organizations: names}
	}
	// Region stations do not expose the organization-list endpoint. Verify
	// against a bound Codeup repository when one exists; this proves both the
	// token and its code-management permission without a project-wide call.
	if repositoryPath != "" {
		if _, repoErr := client.GetRepository(ctx, repositoryPath); repoErr == nil {
			return yunxiaoTokenVerifyResult{Valid: true}
		} else {
			return yunxiaoTokenVerifyResult{Error: repoErr.Error()}
		}
	}
	if _, readErr := client.ListChangeRequests(ctx, nil, "", 1, 1); readErr != nil {
		return yunxiaoTokenVerifyResult{Error: err.Error()}
	}
	return yunxiaoTokenVerifyResult{Valid: true}
}
