package tracker

import (
	"context"
	"fmt"
	"strings"
	"time"

	"agentflow/internal/codeup"
	"agentflow/internal/domain"
)

// ImportCodeupWorkItem pulls one Yunxiao work item and stores it as a platform
// requirement bound to the repository it was imported through.
//
// This is deliberately a one-shot pull, unlike the TAPD import which keeps a
// tracker binding and resyncs: the Codeup API is used here only as a source, and
// the stored issue keeps a read-only reference so the origin stays traceable. A
// later resync would need the Projex project id, which a bare identifier does
// not carry.
func (s *Service) ImportCodeupWorkItem(store IssueStoreAdapter, projectID, repositoryID, identifier, spaceID string) (domain.Issue, error) {
	started := time.Now().UTC()
	projectID = strings.TrimSpace(projectID)
	repositoryID = strings.TrimSpace(repositoryID)
	identifier = strings.TrimSpace(identifier)
	if projectID == "" {
		return domain.Issue{}, fmt.Errorf("projectId is required")
	}
	if identifier == "" {
		return domain.Issue{}, fmt.Errorf("请填写云效工作项编号，例如 DEMO-12")
	}
	if _, ok := store.Project(projectID); !ok {
		return domain.Issue{}, fmt.Errorf("项目 %s 不存在", projectID)
	}
	project, err := s.codeupRepositoryProject(store, projectID, repositoryID)
	if err != nil {
		return domain.Issue{}, err
	}
	handle, err := s.codeupHandle(project)
	if err != nil {
		return domain.Issue{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	item, found, err := handle.client.FindWorkItem(ctx, spaceID, identifier)
	if err != nil {
		s.addLog(domain.SyncLog{ID: fmt.Sprintf("sync-%d", started.UnixNano()), Direction: "pull", Source: domain.CodeupProvider, Action: "import_workitem", Status: "failed", Message: err.Error(), ProjectID: projectID, StartedAt: started, FinishedAt: time.Now().UTC()})
		return domain.Issue{}, err
	}
	if !found {
		return domain.Issue{}, fmt.Errorf("没有找到工作项 %s（编号区分大小写，token 需要有该项目的读权限）", identifier)
	}
	// The user may type the identifier in lower case; store and link the
	// canonical form the API returned.
	if canonical := strings.TrimSpace(item.Identifier); canonical != "" {
		identifier = canonical
	}
	title := strings.TrimSpace(item.Subject)
	if title == "" {
		title = identifier
	}
	description := strings.TrimSpace(item.Description)
	if description == "" {
		description = fmt.Sprintf("从云效导入的工作项 %s。", identifier)
	}
	issue := domain.Issue{
		ProjectID:     projectID,
		Title:         title,
		Description:   description,
		Status:        "backlog",
		Priority:      "medium",
		Kind:          "requirement",
		Source:        domain.CodeupProvider,
		RepositoryID:  repositoryID,
		RepositoryIDs: []string{repositoryID},
		ExternalRef: &domain.ExternalRef{
			Source:        domain.CodeupProvider,
			ExternalID:    identifier,
			ExternalURL:   codeupWorkItemURL(project, item),
			ExternalState: "opened",
			ExternalType:  "workitem",
			SyncedAt:      time.Now().UTC(),
		},
	}
	s.addLog(domain.SyncLog{ID: fmt.Sprintf("sync-%d", started.UnixNano()), Direction: "pull", Source: domain.CodeupProvider, Action: "import_workitem", Status: "success", Count: 1, Message: fmt.Sprintf("导入云效工作项 %s: %s", identifier, title), ProjectID: projectID, StartedAt: started, FinishedAt: time.Now().UTC()})
	return issue, nil
}

// codeupRepositoryProject resolves the Codeup project identity behind an issue
// repository. Importing through a repository the project does not own would
// silently pull from the wrong organization.
func (s *Service) codeupRepositoryProject(store IssueStoreAdapter, projectID, repositoryID string) (string, error) {
	repository, ok := store.Repository(repositoryID)
	if !ok || strings.TrimSpace(repository.ProjectID) != projectID {
		return "", fmt.Errorf("代码仓库 %s 不属于项目 %s", repositoryID, projectID)
	}
	if !isCodeupProject(repository.GongfengProjectID) {
		return "", fmt.Errorf("仓库 %s 不是云效 Codeup 仓库，请先在仓库设置里选择对应的 Codeup 仓库", repository.Name)
	}
	return repository.GongfengProjectID, nil
}

// codeupWorkItemURL builds a clickable link to the work item. Projex links are
// scoped by its own project (space) id, which the search result carries; when it
// is missing the organization landing page is the honest fallback.
func codeupWorkItemURL(project string, item codeup.WorkItem) string {
	info, ok := domain.ParseCodeupRepoURL(project)
	if !ok {
		return ""
	}
	space := strings.TrimSpace(item.SpaceID)
	if space != "" {
		return fmt.Sprintf("https://devops.aliyun.com/projex/organization/%s/project/%s/workitem/%s", info.OrganizationID, space, item.Identifier)
	}
	return fmt.Sprintf("https://devops.aliyun.com/projex/organization/%s", info.OrganizationID)
}
