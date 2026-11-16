package httpapi

import (
	"net/http"
	"strings"

	"agentflow/internal/domain"

	"github.com/gin-gonic/gin"
)

// importCodeup 从云效工作项导入一条需求。
//
// GitHub issue 导入靠 URL 拉取，云效这边用的是「工作项编号」：Projex 的工作项链接
// 里带的是内部 space id，人一般只记得 DEMO-12 这样的编号。仓库决定组织与 space，
// 因此必须先绑定一个 Codeup 仓库。
func (h issuesHandlers) importCodeup(c *gin.Context) {
	var req struct {
		ProjectID          string            `json:"projectId"`
		RepositoryID       string            `json:"repositoryId"`
		RepositoryIDs      []string          `json:"repositoryIds"`
		BaseBranch         *string           `json:"baseBranch"`
		RepositoryBranches map[string]string `json:"repositoryBranches"`
		Identifier         string            `json:"identifier"`
		SpaceID            string            `json:"spaceId"`
	}
	if !decode(c, &req) {
		return
	}
	if strings.TrimSpace(req.ProjectID) == "" || strings.TrimSpace(req.Identifier) == "" {
		c.JSON(http.StatusBadRequest, map[string]string{"error": "请填写项目和云效工作项编号，例如 DEMO-12"})
		return
	}
	if _, ok := h.repo.Project(req.ProjectID); !ok {
		c.JSON(http.StatusBadRequest, map[string]string{"error": "项目不存在"})
		return
	}
	ids := tapdImportRepositoryIDs(req.RepositoryID, req.RepositoryIDs)
	if len(ids) == 0 {
		c.JSON(http.StatusBadRequest, map[string]string{"error": "请至少选择一个代码仓库"})
		return
	}
	repositoryID := ids[0]
	probe := domain.Issue{ProjectID: req.ProjectID, RepositoryIDs: ids, RepositoryID: repositoryID, RepositoryBranches: req.RepositoryBranches}
	if req.BaseBranch != nil {
		probe.BaseBranch = *req.BaseBranch
	}
	if err := h.bindIssueRepositories(&probe); err != nil {
		c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if h.sync == nil {
		c.JSON(http.StatusBadRequest, map[string]string{"error": "同步服务未配置"})
		return
	}
	h.configureSync(h.repo.SyncConfig())
	issue, err := h.sync.ImportCodeupWorkItem(h.repo, req.ProjectID, repositoryID, req.Identifier, req.SpaceID)
	if err != nil {
		respond(c, err)
		return
	}
	// The import returns the record it wants stored; the repository selection and
	// workflow binding still go through the same path a manually created issue
	// takes, so branch handling stays identical.
	previous := issue
	issue.RepositoryIDs = probe.RepositoryIDs
	issue.RepositoryID = probe.RepositoryID
	if req.BaseBranch != nil || req.RepositoryBranches != nil {
		issue.BaseBranch = probe.BaseBranch
		issue.RepositoryBranches = probe.RepositoryBranches
	}
	h.applyTAPDImportRepositories(&issue, previous, probe, req.BaseBranch != nil || req.RepositoryBranches != nil)
	if err := h.bindIssueRepositories(&issue); err != nil {
		c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := h.repo.PutIssue(issue); err != nil {
		respond(c, err)
		return
	}
	stored, _ := h.repo.Issue(issue.ID)
	c.JSON(http.StatusOK, stored)
}
