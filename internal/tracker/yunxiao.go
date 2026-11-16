package tracker

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"agentflow/internal/domain"
	"agentflow/internal/yunxiao"
)

// yunxiaoClientForProject builds a client with the PAT of userID (or the
// project owner when userID is zero). It never falls back to a platform-wide
// token: every Yunxiao call is attributed to a person who has access.
func (s *Service) yunxiaoClientForProject(projectID string, userID int64) (*yunxiao.Client, domain.Project, domain.User, error) {
	if s.projectAuth == nil {
		return nil, domain.Project{}, domain.User{}, fmt.Errorf("云效同步需要项目与用户存储")
	}
	projectID = strings.TrimSpace(projectID)
	project, ok := s.projectAuth.Project(projectID)
	if !ok {
		return nil, domain.Project{}, domain.User{}, fmt.Errorf("项目 %s 不存在", projectID)
	}
	if userID <= 0 {
		userID = project.OwnerID
	}
	if userID <= 0 {
		return nil, project, domain.User{}, fmt.Errorf("项目 %s 未设置 Owner，无法确定云效令牌", project.ID)
	}
	user, ok := s.projectAuth.User(userID)
	if !ok {
		return nil, project, domain.User{}, fmt.Errorf("项目 %s 的用户 %d 不存在", project.ID, userID)
	}
	token := strings.TrimSpace(user.YunxiaoToken)
	if token == "" {
		return nil, project, user, fmt.Errorf("项目 %s 的责任人未配置云效访问令牌", project.ID)
	}
	cfg := s.cfg.Get()
	base := strings.TrimSpace(cfg.YunxiaoAPIBase)
	if base == "" {
		return nil, project, user, fmt.Errorf("平台尚未配置云效 API 地址")
	}
	if strings.TrimSpace(cfg.YunxiaoOrganizationID) == "" && strings.Contains(strings.ToLower(base), "openapi-rdc.aliyuncs.com") {
		return nil, project, user, fmt.Errorf("平台尚未配置云效组织 ID")
	}
	return yunxiao.New(base, cfg.YunxiaoOrganizationID, token), project, user, nil
}

// yunxiaoClientForProjectOrOwner prefers the given user's PAT and falls back to
// the project owner for write-back paths where the assignee may not have one.
func (s *Service) yunxiaoClientForProjectOrOwner(projectID string, userID int64) (*yunxiao.Client, domain.Project, domain.User, error) {
	client, project, user, err := s.yunxiaoClientForProject(projectID, userID)
	if err == nil || userID <= 0 {
		return client, project, user, err
	}
	return s.yunxiaoClientForProject(projectID, 0)
}

// yunxiaoClientForRemoteProject resolves a "codeup:{org}/{path}" delivery
// identity back to its repository and project owner.
func (s *Service) yunxiaoClientForRemoteProject(remote string) (*yunxiao.Client, domain.Repository, error) {
	if s.projectAuth == nil {
		return nil, domain.Repository{}, fmt.Errorf("云效仓库需要项目与用户存储")
	}
	remote = strings.TrimSpace(remote)
	for _, repository := range s.projectAuth.Repositories() {
		if repository.Provider != domain.RepositoryProviderYunxiao {
			continue
		}
		if strings.TrimSpace(repository.GongfengProjectID) != remote {
			continue
		}
		client, _, _, err := s.yunxiaoClientForProject(repository.ProjectID, 0)
		if err != nil {
			return nil, repository, err
		}
		return client, repository, nil
	}
	return nil, domain.Repository{}, fmt.Errorf("云效仓库 %s 未绑定本地项目", remote)
}

func (s *Service) yunxiaoRepositoryID(ctx context.Context, client *yunxiao.Client, repository domain.Repository) (int, error) {
	if repository.YunxiaoRepoID > 0 {
		return repository.YunxiaoRepoID, nil
	}
	remote := strings.TrimSpace(repository.GongfengProjectID)
	if cached, ok := s.yunxiaoRepoIDs.Load(remote); ok {
		if id, ok := cached.(int); ok && id > 0 {
			return id, nil
		}
	}
	path := strings.TrimPrefix(remote, "codeup:")
	if path == "" {
		return 0, fmt.Errorf("云效仓库标识为空")
	}
	info, err := client.GetRepository(ctx, path)
	if err != nil {
		return 0, err
	}
	if info.ID <= 0 {
		return 0, fmt.Errorf("云效没有返回仓库 %s 的数字 ID", remote)
	}
	s.yunxiaoRepoIDs.Store(remote, info.ID)
	return info.ID, nil
}

func yunxiaoMRState(raw string) string {
	switch strings.ToUpper(strings.TrimSpace(raw)) {
	case "UNDER_DEV", "UNDER_REVIEW", "TO_BE_MERGED", "OPENED", "REOPENED", "ACCEPTED", "LOCKED":
		return "opened"
	case "MERGED":
		return "merged"
	case "CLOSED":
		return "closed"
	default:
		return ""
	}
}

func yunxiaoMergedAt(raw string) time.Time {
	if strings.TrimSpace(raw) == "" {
		return time.Time{}
	}
	// Codeup does not expose a dedicated merged_at field. updateTime is the
	// closest remote timestamp; never use the local poll time.
	if t, err := time.Parse(time.RFC3339, strings.TrimSpace(raw)); err == nil {
		return t.UTC()
	}
	return time.Time{}
}

// findYunxiaoMR returns the newest matching change request. With
// includeClosed=false it only looks at opened MRs (the create/reuse path);
// with true it also finds merged and closed rows for delivery state.
func (s *Service) findYunxiaoMR(ctx context.Context, client *yunxiao.Client, repoID int, project, source, target string, includeClosed bool) (domain.DeliveryMR, bool, error) {
	states := []string{"opened"}
	if includeClosed {
		states = []string{"opened", "merged", "closed"}
	}
	for _, state := range states {
		items, err := client.ListAllChangeRequests(ctx, []int{repoID}, state, 100, 10)
		if err != nil {
			return domain.DeliveryMR{}, false, err
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
			continue
		}
		url := strings.TrimSpace(best.WebURL)
		if url == "" {
			url = domain.MRURL("yunxiao", project, best.LocalID)
		}
		mr := domain.DeliveryMR{
			Project:       project,
			IID:           best.LocalID,
			URL:           url,
			TargetBranch:  best.TargetBranch,
			State:         yunxiaoMRState(best.RawStatus()),
			GongfengState: yunxiaoMRState(best.RawStatus()),
		}
		if mr.State == "" {
			mr.State = "unknown"
		}
		return mr, true, nil
	}
	return domain.DeliveryMR{}, false, nil
}

func (s *Service) yunxiaoFindBranchMR(project, sourceBranch string) (domain.DeliveryMR, bool, error) {
	client, repository, err := s.yunxiaoClientForRemoteProject(project)
	if err != nil {
		return domain.DeliveryMR{}, false, err
	}
	repoID, err := s.yunxiaoRepositoryID(context.Background(), client, repository)
	if err != nil {
		return domain.DeliveryMR{}, false, err
	}
	return s.findYunxiaoMR(context.Background(), client, repoID, project, sourceBranch, "", true)
}

func (s *Service) yunxiaoEnsureAndMergeBranchMR(project, sourceBranch, targetBranch, title, description string) (string, error) {
	client, repository, err := s.yunxiaoClientForRemoteProject(project)
	if err != nil {
		return "", err
	}
	repoID, err := s.yunxiaoRepositoryID(context.Background(), client, repository)
	if err != nil {
		return "", err
	}
	source := strings.TrimSpace(sourceBranch)
	target := strings.TrimSpace(targetBranch)
	if source == "" {
		return "", fmt.Errorf("source branch is required")
	}
	if target != "" && source == target {
		return "", nil
	}
	ctx := context.Background()
	open, found, err := s.findYunxiaoMR(ctx, client, repoID, project, source, target, false)
	if err != nil {
		return "", err
	}
	if !found {
		if target == "" {
			return "", ErrNoOpenMR
		}
		createTitle := strings.TrimSpace(title)
		if createTitle == "" {
			createTitle = fmt.Sprintf("LoopForge: %s → %s", source, target)
		}
		createDesc := strings.TrimSpace(description)
		if createDesc == "" {
			createDesc = fmt.Sprintf("定时 SOP 将 `%s` 合入 `%s`。", source, target)
		}
		created, createErr := client.CreateChangeRequest(ctx, repoID, yunxiao.CreateChangeRequestInput{
			SourceBranch: source,
			TargetBranch: target,
			Title:        truncateTitle(createTitle),
			Description:  truncateDescription(createDesc),
		})
		// Search again even when create returned an error: another instance may
		// have won the race and left the reusable MR open.
		var searchErr error
		open, found, searchErr = s.findYunxiaoMR(ctx, client, repoID, project, source, target, false)
		if searchErr != nil {
			return "", searchErr
		}
		if !found {
			if createErr != nil {
				return "", fmt.Errorf("create MR %s → %s: %w", source, target, createErr)
			}
			if created.LocalID <= 0 {
				return "", fmt.Errorf("create_change_request did not return a local id")
			}
			open = domain.DeliveryMR{Project: project, IID: created.LocalID, URL: created.WebURL, TargetBranch: target, State: "opened"}
			found = true
		}
	}
	if open.IID <= 0 {
		return "", fmt.Errorf("create_change_request did not return a local id")
	}
	if _, err := client.MergeChangeRequest(ctx, repoID, open.IID, yunxiao.MergeChangeRequestInput{MergeType: yunxiao.MergeTypeNoFastForward}); err != nil {
		return "", fmt.Errorf("merge MR #%d: %w", open.IID, err)
	}
	return "#" + strconv.Itoa(open.IID), nil
}

func truncateTitle(value string) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) <= 256 {
		return string(runes)
	}
	return string(runes[:256])
}

func truncateDescription(value string) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) <= 10000 {
		return string(runes)
	}
	return string(runes[:10000])
}

func (s *Service) refreshYunxiaoDelivery(store MRDeliveryStore, project string, localID int) error {
	client, repository, err := s.yunxiaoClientForRemoteProject(project)
	if err != nil {
		return err
	}
	repoID, err := s.yunxiaoRepositoryID(context.Background(), client, repository)
	if err != nil {
		return err
	}
	cr, err := client.GetChangeRequest(context.Background(), repoID, localID)
	if err != nil {
		return err
	}
	state := yunxiaoMRState(cr.RawStatus())
	if state == "" || state == "unknown" {
		return nil
	}
	mergedAt := time.Time{}
	if state == "merged" {
		mergedAt = yunxiaoMergedAt(cr.UpdateTime)
	}
	return store.UpdateMRDeliveryState(project, localID, state, strings.TrimSpace(cr.TargetBranch), mergedAt, time.Now().UTC())
}

// --- Projex workitems ---

func (s *Service) pullAllYunxiao(store IssueStoreAdapter) (int, error) {
	seen := map[string]bool{}
	total := 0
	var lastErr error
	for _, project := range store.Projects() {
		spaceID := strings.TrimSpace(project.YunxiaoSpaceID)
		if spaceID == "" {
			continue
		}
		key := spaceID + "|" + strings.TrimSpace(project.YunxiaoSprintID)
		if seen[key] {
			continue
		}
		seen[key] = true
		n, err := s.pullYunxiaoWorkitems(store, project)
		total += n
		if err != nil {
			lastErr = err
		}
	}
	return total, lastErr
}

func (s *Service) pullYunxiaoWorkitems(store IssueStoreAdapter, project domain.Project) (int, error) {
	started := time.Now().UTC()
	spaceID := strings.TrimSpace(project.YunxiaoSpaceID)
	if spaceID == "" {
		return 0, nil
	}
	client, _, _, err := s.yunxiaoClientForProject(project.ID, 0)
	if err != nil {
		s.addLog(domain.SyncLog{ID: fmt.Sprintf("sync-%d", started.UnixNano()), Direction: "pull", Source: "yunxiao", Action: "sync_issues", Status: "failed", Message: err.Error(), ProjectID: project.ID, StartedAt: started, FinishedAt: time.Now().UTC()})
		return 0, err
	}
	categories := yunxiaoCategories(project)
	items, err := client.ListAllWorkitems(context.Background(), yunxiao.SearchWorkitemsInput{
		Category:  categories,
		SpaceID:   spaceID,
		SpaceType: "Project",
		OrderBy:   "gmtCreate",
		Sort:      "desc",
		PerPage:   200,
	}, 50)
	if err != nil {
		s.addLog(domain.SyncLog{ID: fmt.Sprintf("sync-%d", started.UnixNano()), Direction: "pull", Source: "yunxiao", Action: "sync_issues", Status: "failed", Message: err.Error(), ProjectID: project.ID, StartedAt: started, FinishedAt: time.Now().UTC()})
		return 0, err
	}
	count := 0
	for _, item := range items {
		if strings.TrimSpace(item.ID) == "" {
			continue
		}
		existing, found := findYunxiaoIssue(store, project.ID, item.ID)
		issue := s.yunxiaoSyncedIssue(project, item, existing, found)
		if err := store.PutSyncedIssue(issue); err != nil {
			log.Printf("sync yunxiao workitem %s: %v", item.ID, err)
			continue
		}
		count++
	}
	message := fmt.Sprintf("同步 %d 条云效工作项", count)
	if len(items) == 0 {
		message = "云效未返回工作项"
	}
	s.addLog(domain.SyncLog{ID: fmt.Sprintf("sync-%d", started.UnixNano()), Direction: "pull", Source: "yunxiao", Action: "sync_issues", Status: "success", Count: count, Message: message, ProjectID: project.ID, StartedAt: started, FinishedAt: time.Now().UTC()})
	return count, nil
}

func yunxiaoCategories(project domain.Project) string {
	if len(project.YunxiaoWorkItemTypes) == 0 {
		return "Req,Bug"
	}
	hasReq, hasBug := false, false
	for key := range project.YunxiaoWorkItemTypes {
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "req", "requirement":
			hasReq = true
		case "bug", "issue":
			hasBug = true
		}
	}
	if hasReq && hasBug {
		return "Req,Bug"
	}
	if hasReq {
		return "Req"
	}
	if hasBug {
		return "Bug"
	}
	return "Req,Bug"
}

func (s *Service) yunxiaoSyncedIssue(project domain.Project, item yunxiao.Workitem, existing domain.Issue, found bool) domain.Issue {
	issueID := yunxiaoIssueID(project.ID, item.ID)
	if found {
		issueID = existing.ID
	}
	status := yunxiaoStatus(project, item.Status)
	issue := domain.Issue{
		ID:             issueID,
		ProjectID:      project.ID,
		Kind:           yunxiaoKind(item.CategoryID),
		Title:          strings.TrimSpace(item.Subject),
		Description:    item.Description,
		Priority:       yunxiaoPriority(item.CustomFieldValues),
		Status:         status,
		CloseSource:    closeSourceForStatus(status),
		AssigneeUserID: s.yunxiaoAssigneeUserID(item.AssignedTo),
		Source:         "yunxiao",
		ExternalRef: &domain.ExternalRef{
			Source:        "yunxiao",
			ExternalID:    strings.TrimSpace(item.ID),
			ExternalURL:   yunxiaoWorkitemURL(project, item),
			ExternalState: yunxiaoExternalState(item.Status),
			ExternalType:  yunxiaoExternalType(item),
			SyncedAt:      time.Now().UTC(),
		},
	}
	if found {
		applyExistingIssue(&issue, existing)
	} else {
		issue.CreatedAt = parseTime(item.CreatedAt)
	}
	issue.UpdatedAt = time.Now().UTC()
	return issue
}

func (s *Service) yunxiaoAssigneeUserID(user yunxiao.User) int64 {
	if s.projectAuth == nil {
		return 0
	}
	directory, ok := s.projectAuth.(interface{ Users() []domain.User })
	if !ok {
		return 0
	}
	id := strings.TrimSpace(user.ID)
	name := strings.TrimSpace(user.Name)
	for _, candidate := range directory.Users() {
		if id != "" && strings.TrimSpace(candidate.YunxiaoUserID) == id {
			return candidate.ID
		}
	}
	if name == "" {
		return 0
	}
	for _, candidate := range directory.Users() {
		if strings.EqualFold(strings.TrimSpace(candidate.LoginName), name) ||
			strings.EqualFold(strings.TrimSpace(candidate.ChineseName), name) {
			return candidate.ID
		}
	}
	return 0
}

func yunxiaoIssueID(projectID, externalID string) string {
	return "yx-" + safeIssueID(projectID) + "-" + safeIssueID(externalID)
}

func safeIssueID(value string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(value) {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "unknown"
	}
	return out
}

func findYunxiaoIssue(store IssueStoreAdapter, projectID, externalID string) (domain.Issue, bool) {
	stable := yunxiaoIssueID(projectID, externalID)
	if issue, ok := store.Issue(stable); ok && issue.Source == "yunxiao" {
		return issue, true
	}
	for _, issue := range store.Issues() {
		if issue.Source != "yunxiao" || issue.ExternalRef == nil {
			continue
		}
		if issue.ProjectID == projectID && strings.TrimSpace(issue.ExternalRef.ExternalID) == strings.TrimSpace(externalID) {
			return issue, true
		}
	}
	return domain.Issue{}, false
}

func yunxiaoKind(category string) string {
	if strings.EqualFold(strings.TrimSpace(category), "Req") {
		return "requirement"
	}
	return "issue"
}

func yunxiaoExternalType(item yunxiao.Workitem) string {
	if strings.TrimSpace(item.CategoryID) != "" {
		return strings.TrimSpace(item.CategoryID)
	}
	if strings.TrimSpace(item.WorkitemType.Name) != "" {
		return strings.TrimSpace(item.WorkitemType.Name)
	}
	return "Req"
}

func yunxiaoExternalState(status yunxiao.WorkitemStatus) string {
	for _, value := range []string{status.NameEn, status.Name, status.ID} {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func yunxiaoStatus(project domain.Project, status yunxiao.WorkitemStatus) string {
	for _, key := range []string{status.ID, status.NameEn, status.Name} {
		if key == "" {
			continue
		}
		if mapped := strings.TrimSpace(project.YunxiaoStatusMap[key]); mapped != "" {
			return normalizeLocalStatus(mapped)
		}
	}
	raw := strings.ToLower(strings.TrimSpace(status.NameEn))
	if raw == "" {
		raw = strings.ToLower(strings.TrimSpace(status.Name))
	}
	switch raw {
	case "todo", "new", "待处理", "新建":
		return "todo"
	case "in_progress", "doing", "processing", "处理中", "开发中":
		return "in_progress"
	case "in_review", "review", "resolved", "待评审", "已解决":
		return "in_review"
	case "done", "completed", "已完成", "完成":
		return "done"
	case "closed", "已关闭":
		return "closed"
	case "rejected", "cancelled", "canceled", "已拒绝", "取消":
		return "cancelled"
	default:
		return "backlog"
	}
}

func normalizeLocalStatus(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "backlog", "todo", "in_progress", "in_review", "done", "closed", "blocked", "cancelled":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "backlog"
	}
}

func yunxiaoPriority(fields []yunxiao.CustomField) string {
	for _, field := range fields {
		name := strings.ToLower(strings.TrimSpace(field.FieldName + " " + field.FieldID))
		if !strings.Contains(name, "priority") && !strings.Contains(name, "优先级") {
			continue
		}
		value := strings.ToLower(strings.TrimSpace(field.DisplayValue))
		if value == "" {
			value = strings.ToLower(strings.TrimSpace(field.Value))
		}
		switch value {
		case "p0", "urgent", "紧急", "最高":
			return "urgent"
		case "p1", "high", "高":
			return "high"
		case "p2", "medium", "中":
			return "medium"
		case "p3", "low", "低":
			return "low"
		}
	}
	return "medium"
}

func yunxiaoWorkitemURL(project domain.Project, item yunxiao.Workitem) string {
	spaceID := strings.TrimSpace(project.YunxiaoSpaceID)
	id := strings.TrimSpace(item.ID)
	if spaceID == "" || id == "" {
		return ""
	}
	category := strings.ToLower(strings.TrimSpace(item.CategoryID))
	if category == "" {
		category = "req"
	}
	return fmt.Sprintf("https://devops.aliyun.com/projex/project/%s/%s/%s", spaceID, category, id)
}

func (s *Service) CreateYunxiaoWorkitem(store IssueStoreAdapter, issue domain.Issue) (domain.Issue, error) {
	started := time.Now().UTC()
	project, ok := store.Project(issue.ProjectID)
	if !ok {
		return issue, fmt.Errorf("project not found")
	}
	spaceID := strings.TrimSpace(project.YunxiaoSpaceID)
	if spaceID == "" {
		return issue, fmt.Errorf("项目 %s 未配置云效空间 ID", project.ID)
	}
	category := yunxiaoCreateCategory(project, issue)
	typeID := strings.TrimSpace(project.YunxiaoWorkItemTypes[category])
	if typeID == "" {
		return issue, fmt.Errorf("项目 %s 未配置云效工作项类型 %s", project.ID, category)
	}
	assignee, err := s.yunxiaoAssignee(project, issue)
	if err != nil {
		return issue, err
	}
	client, _, _, err := s.yunxiaoClientForProjectOrOwner(project.ID, issue.AssigneeUserID)
	if err != nil {
		return issue, err
	}
	createdID, err := client.CreateWorkitem(context.Background(), yunxiao.CreateWorkitemInput{
		AssignedTo:     assignee,
		SpaceID:        spaceID,
		WorkitemTypeID: typeID,
		Subject:        strings.TrimSpace(issue.Title),
		Description:    issue.Description,
		FormatType:     "MARKDOWN",
	})
	if err != nil {
		s.addLog(domain.SyncLog{ID: fmt.Sprintf("sync-%d", started.UnixNano()), Direction: "push", Source: "yunxiao", Action: "create_issue", Status: "failed", Message: err.Error(), ProjectID: project.ID, StartedAt: started, FinishedAt: time.Now().UTC()})
		return issue, err
	}
	item, err := client.GetWorkitem(context.Background(), createdID)
	if err != nil {
		return issue, err
	}
	issue.Source = "yunxiao"
	issue.Kind = yunxiaoKind(category)
	issue.ExternalRef = &domain.ExternalRef{
		Source:        "yunxiao",
		ExternalID:    createdID,
		ExternalURL:   yunxiaoWorkitemURL(project, item),
		ExternalState: yunxiaoExternalState(item.Status),
		ExternalType:  yunxiaoExternalType(item),
		SyncedAt:      time.Now().UTC(),
	}
	stable := yunxiaoIssueID(project.ID, createdID)
	oldID := issue.ID
	issue.ID = stable
	if err := store.PutIssue(issue); err != nil {
		return issue, err
	}
	if oldID != "" && oldID != stable && strings.HasPrefix(oldID, "issue-") {
		_ = store.DeleteIssue(oldID)
	}
	s.addLog(domain.SyncLog{ID: fmt.Sprintf("sync-%d", started.UnixNano()), Direction: "push", Source: "yunxiao", Action: "create_issue", Status: "success", Count: 1, Message: fmt.Sprintf("创建云效工作项 %s: %s", createdID, issue.Title), ProjectID: project.ID, StartedAt: started, FinishedAt: time.Now().UTC()})
	return issue, nil
}

func yunxiaoCreateCategory(project domain.Project, issue domain.Issue) string {
	if issue.Kind == "issue" || issue.Kind == "bug" {
		if _, ok := project.YunxiaoWorkItemTypes["Bug"]; ok {
			return "Bug"
		}
	}
	if _, ok := project.YunxiaoWorkItemTypes["Req"]; ok {
		return "Req"
	}
	for key := range project.YunxiaoWorkItemTypes {
		if strings.EqualFold(key, "Req") {
			return key
		}
	}
	for key := range project.YunxiaoWorkItemTypes {
		if strings.EqualFold(key, "Bug") {
			return key
		}
	}
	return "Req"
}

func (s *Service) yunxiaoAssignee(project domain.Project, issue domain.Issue) (string, error) {
	if issue.AssigneeUserID > 0 && s.projectAuth != nil {
		if user, ok := s.projectAuth.User(issue.AssigneeUserID); ok {
			if id := strings.TrimSpace(user.YunxiaoUserID); id != "" {
				return id, nil
			}
		}
	}
	if id := strings.TrimSpace(project.YunxiaoDefaultAssigneeID); id != "" {
		return id, nil
	}
	if project.OwnerID > 0 && s.projectAuth != nil {
		if owner, ok := s.projectAuth.User(project.OwnerID); ok {
			if id := strings.TrimSpace(owner.YunxiaoUserID); id != "" {
				return id, nil
			}
		}
	}
	return "", fmt.Errorf("无法确定云效工作项负责人：请给责任人配置云效用户 ID，或设置项目默认负责人")
}
