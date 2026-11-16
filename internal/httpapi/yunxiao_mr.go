package httpapi

import (
	"context"
	"fmt"
	"strings"

	"agentflow/internal/domain"
	"agentflow/internal/yunxiao"
)

// createYunxiaoMR creates or reuses one Codeup merge request for a conversation
// workspace. The caller has already pushed the work branch with SSH.
func (h *agentToolGateway) createYunxiaoMR(ctx context.Context, token string, rc domain.RepositoryContext, source, target, title, description string) (yunxiao.ChangeRequest, bool, error) {
	repository, ok := h.repo.Repository(rc.RepositoryID)
	if !ok || repository.Provider != domain.RepositoryProviderYunxiao {
		return yunxiao.ChangeRequest{}, false, fmt.Errorf("云效仓库 %s 未在项目中登记", rc.Name)
	}
	cfg := h.repo.SyncConfig()
	client, err := yunxiao.NewChecked(cfg.YunxiaoAPIBase, cfg.YunxiaoOrganizationID, token)
	if err != nil {
		return yunxiao.ChangeRequest{}, false, err
	}
	repoID := repository.YunxiaoRepoID
	if repoID <= 0 {
		path := strings.TrimPrefix(strings.TrimSpace(repository.GongfengProjectID), "codeup:")
		info, err := client.GetRepository(ctx, path)
		if err != nil {
			return yunxiao.ChangeRequest{}, false, err
		}
		if info.ID <= 0 {
			return yunxiao.ChangeRequest{}, false, fmt.Errorf("云效没有返回仓库 %s 的数字 ID", repository.GongfengProjectID)
		}
		repoID = info.ID
		repository.YunxiaoRepoID = info.ID
		if strings.TrimSpace(repository.WebURL) == "" {
			repository.WebURL = strings.TrimSpace(info.WebURL)
		}
		if strings.TrimSpace(info.SSHURL) != "" {
			repository.CloneURL = strings.TrimSpace(info.SSHURL)
		}
		_ = h.repo.PutRepository(repository)
	}
	findOpen := func() (yunxiao.ChangeRequest, bool, error) {
		items, err := client.ListAllChangeRequests(ctx, []int{repoID}, "opened", 100, 10)
		if err != nil {
			return yunxiao.ChangeRequest{}, false, err
		}
		var best *yunxiao.ChangeRequest
		for i := range items {
			item := &items[i]
			if !domain.SameGitBranch(item.SourceBranch, source) {
				continue
			}
			if target != "" && !domain.SameGitBranch(item.TargetBranch, target) {
				continue
			}
			if best == nil || item.LocalID > best.LocalID {
				best = item
			}
		}
		if best == nil {
			return yunxiao.ChangeRequest{}, false, nil
		}
		return *best, true, nil
	}
	if existing, found, err := findOpen(); err != nil {
		return yunxiao.ChangeRequest{}, false, err
	} else if found {
		return existing, true, nil
	}
	created, err := client.CreateChangeRequest(ctx, repoID, yunxiao.CreateChangeRequestInput{
		SourceBranch: source,
		TargetBranch: target,
		Title:        truncateRunes(strings.TrimSpace(title), 256),
		Description:  truncateRunes(strings.TrimSpace(description), 10000),
	})
	if err != nil {
		// Another instance may have created the same MR between our list and
		// create calls; reuse it instead of reporting a duplicate-create error.
		if existing, found, findErr := findOpen(); findErr == nil && found {
			return existing, true, nil
		}
		return yunxiao.ChangeRequest{}, false, err
	}
	return created, false, nil
}
