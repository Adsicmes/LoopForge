package tracker

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"agentflow/internal/codeup"
	"agentflow/internal/domain"
)

// codeupAPIBaseEnv overrides the Yunxiao OpenAPI host. Region editions use
// their own domain, so a deployment sets it once for the whole platform.
const codeupAPIBaseEnv = "AGENTFLOW_CODEUP_API_BASE"

// codeupProjectPrefix marks a Codeup repository identity, mirroring the
// "github:" prefix the GitHub adapter uses.
const codeupProjectPrefix = "codeup:"

func isCodeupProject(project string) bool {
	return strings.HasPrefix(strings.TrimSpace(project), codeupProjectPrefix)
}

// codeupClient builds a Yunxiao client for one Codeup repository. The token is
// the responsible user's Git credential, exactly like the GitHub adapter: the
// personal access token lives in the user's credential slot, never in the
// repository row.
func (s *Service) codeupClient(project string) (*codeup.Client, error) {
	info, ok := domain.ParseCodeupRepoURL(project)
	if !ok || info.Project != strings.TrimSpace(project) {
		return nil, fmt.Errorf("无效的 Codeup 代码库标识：%s", project)
	}
	var token string
	if s.projectAuth != nil {
		for _, repository := range s.projectAuth.Repositories() {
			if repository.Provider != domain.CodeupProvider || repository.GongfengProjectID != project {
				continue
			}
			if p, found := s.projectAuth.Project(repository.ProjectID); found {
				if u, exists := s.projectAuth.User(p.OwnerID); exists {
					token = u.GitTokenFor(domain.GitProviderCodeup)
				}
			}
			break
		}
	}
	if strings.TrimSpace(token) == "" {
		return nil, fmt.Errorf("项目负责人还没有配置云效个人访问令牌（个人设置 → Git Token），无法操作 Codeup 合并请求")
	}
	return codeup.New(codeup.Config{
		APIBase:        s.codeupAPIBase(),
		OrganizationID: info.OrganizationID,
		Token:          token,
	})
}

// codeupAPIBase is the injected test base, then the deployment override, then
// the central-edition default.
func (s *Service) codeupAPIBase() string {
	if s != nil && strings.TrimSpace(s.codeupAPIBaseURL) != "" {
		return s.codeupAPIBaseURL
	}
	return strings.TrimSpace(os.Getenv(codeupAPIBaseEnv))
}

// codeupRepository resolves a Codeup path to the numeric repository id the
// change-request APIs expect.
func (c *codeupClientHandle) repository(ctx context.Context) (codeup.Repository, error) {
	if c.cached != nil {
		return *c.cached, nil
	}
	repo, err := c.client.GetRepository(ctx, c.path)
	if err != nil {
		return codeup.Repository{}, fmt.Errorf("查询 Codeup 代码库失败：%w", err)
	}
	c.cached = &repo
	return repo, nil
}

type codeupClientHandle struct {
	client *codeup.Client
	path   string
	cached *codeup.Repository
}

// codeupHandle resolves both the client and the repository identity for a
// project in one place.
func (s *Service) codeupHandle(project string) (*codeupClientHandle, error) {
	info, ok := domain.ParseCodeupRepoURL(project)
	if !ok {
		return nil, fmt.Errorf("无效的 Codeup 代码库标识：%s", project)
	}
	client, err := s.codeupClient(project)
	if err != nil {
		return nil, err
	}
	return &codeupClientHandle{client: client, path: info.Path}, nil
}

// codeupMRState maps the Yunxiao change-request status onto the platform's
// delivery states (opened / merged / closed).
func codeupMRState(status string) string {
	switch strings.ToUpper(strings.TrimSpace(status)) {
	case codeup.StateMerged:
		return "merged"
	case codeup.StateClosed:
		return "closed"
	case codeup.StateUnderDev, codeup.StateUnderReview, codeup.StateToBeMerged:
		return "opened"
	default:
		return ""
	}
}

func codeupMRURL(project string, localID int64) string {
	path := domain.CodeupRepositoryPath(project)
	if path == "" {
		return ""
	}
	return fmt.Sprintf("https://%s/%s/change/%d", domain.CodeupHost, path, localID)
}

func codeupDelivery(project string, cr codeup.ChangeRequest) domain.DeliveryMR {
	state := codeupMRState(cr.Status)
	url := strings.TrimSpace(cr.DetailURL)
	if url == "" {
		url = codeupMRURL(project, cr.LocalID)
	}
	return domain.DeliveryMR{
		Project:       project,
		IID:           int(cr.LocalID),
		URL:           url,
		State:         state,
		GongfengState: state,
		TargetBranch:  cr.TargetBranch,
	}
}

// codeupPullTo is milliseconds; the merge call returns before Yunxiao has
// finished merging, so the caller re-reads the request a few times.
const (
	codeupMergeAttempts = 6
	codeupMergeDelay    = 2 * time.Second
)

// codeupFindBranch returns the most recent change request whose source branch
// matches. The OpenAPI filters by project, not by branch, so the match happens
// here.
func (s *Service) codeupFindBranch(project, branch string) (domain.DeliveryMR, bool, error) {
	handle, err := s.codeupHandle(project)
	if err != nil {
		return domain.DeliveryMR{}, false, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	repo, err := handle.repository(ctx)
	if err != nil {
		return domain.DeliveryMR{}, false, err
	}
	items, err := handle.client.ListChangeRequests(ctx, []string{strconv.FormatInt(repo.ID, 10)}, "", 1, 100)
	if err != nil {
		return domain.DeliveryMR{}, false, err
	}
	for _, item := range items {
		if strings.TrimSpace(item.SourceBranch) != strings.TrimSpace(branch) {
			continue
		}
		return codeupDelivery(project, item), true, nil
	}
	return domain.DeliveryMR{}, false, nil
}

// codeupMergeBranch finds or opens a change request from source to target and
// merges it. Yunxiao requires both project ids even for a same-repository
// merge request, and it merges asynchronously, so the result is re-read until
// it reports MERGED or the attempt budget runs out.
func (s *Service) codeupMergeBranch(project, branch, target, title, description string) (string, error) {
	branch = strings.TrimSpace(branch)
	target = strings.TrimSpace(target)
	if branch == "" {
		return "", fmt.Errorf("source branch is required")
	}
	if branch == target {
		return "", nil
	}
	handle, err := s.codeupHandle(project)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	repo, err := handle.repository(ctx)
	if err != nil {
		return "", err
	}
	items, err := handle.client.ListChangeRequests(ctx, []string{strconv.FormatInt(repo.ID, 10)}, "", 1, 100)
	if err != nil {
		return "", err
	}
	var found *codeup.ChangeRequest
	for i := range items {
		item := items[i]
		if strings.TrimSpace(item.SourceBranch) != branch {
			continue
		}
		if target != "" && strings.TrimSpace(item.TargetBranch) != target {
			continue
		}
		found = &item
		break
	}
	if found != nil && found.Merged() {
		return "#" + strconv.FormatInt(found.LocalID, 10), nil
	}
	if found == nil || found.Closed() {
		if target == "" {
			return "", ErrNoOpenMR
		}
		createTitle := strings.TrimSpace(title)
		if createTitle == "" {
			createTitle = fmt.Sprintf("LoopForge: %s → %s", branch, target)
		}
		created, createErr := handle.client.CreateChangeRequest(ctx, codeup.CreateChangeRequestInput{
			RepositoryID:    strconv.FormatInt(repo.ID, 10),
			SourceProjectID: repo.ID,
			TargetProjectID: repo.ID,
			SourceBranch:    branch,
			TargetBranch:    target,
			Title:           createTitle,
			Description:     description,
		})
		if createErr != nil {
			return "", createErr
		}
		found = &created
	}
	if err := handle.client.MergeChangeRequest(ctx, strconv.FormatInt(repo.ID, 10), found.LocalID, codeup.MergeTypeNoFastForward); err != nil {
		return "", err
	}
	for attempt := 0; attempt < codeupMergeAttempts; attempt++ {
		current, readErr := handle.client.GetChangeRequest(ctx, strconv.FormatInt(repo.ID, 10), found.LocalID)
		if readErr == nil {
			if current.Merged() {
				return "#" + strconv.FormatInt(current.LocalID, 10), nil
			}
			if current.Closed() {
				return "", fmt.Errorf("Codeup 合并请求 #%d 已被关闭，未合并", current.LocalID)
			}
		}
		if attempt == codeupMergeAttempts-1 {
			break
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(codeupMergeDelay):
		}
	}
	return "", fmt.Errorf("Codeup 未在预期时间内完成合并请求 #%d 的合并（可能仍在等待评审或卡点检查）", found.LocalID)
}

// refreshCodeupDelivery re-reads one change request and stores its state.
func (s *Service) refreshCodeupDelivery(store MRDeliveryStore, project string, localID int) error {
	if localID <= 0 {
		return fmt.Errorf("invalid Codeup change request id")
	}
	handle, err := s.codeupHandle(project)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	repo, err := handle.repository(ctx)
	if err != nil {
		return err
	}
	current, err := handle.client.GetChangeRequest(ctx, strconv.FormatInt(repo.ID, 10), int64(localID))
	if err != nil {
		return err
	}
	state := codeupMRState(current.Status)
	if state == "" {
		return nil
	}
	// The change-request detail API does not expose a merge timestamp, so the
	// poll time is the best available value; rows already merged keep theirs.
	var mergedAt time.Time
	if state == "merged" {
		mergedAt = time.Now().UTC()
	}
	return store.UpdateMRDeliveryState(project, int(current.LocalID), state, current.TargetBranch, mergedAt, time.Now().UTC())
}
