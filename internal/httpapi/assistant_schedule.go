package httpapi

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"agentflow/internal/domain"
)

func (h *assistantHandlers) prepareScheduleJob(job domain.ScheduleJob) (string, error) {
	if h == nil || h.repo == nil {
		return "", fmt.Errorf("未连接数据源")
	}
	prompt := strings.TrimSpace(job.AssistantPrompt)
	if job.SharedTaskID != "" {
		version, err := h.repo.SharedScheduleVersion(job.SharedTaskID, job.SharedTaskVersion)
		if err != nil {
			return "", fmt.Errorf("读取订阅版本失败：%w", err)
		}
		prompt, _, err = version.Render(job.ParameterValues)
		if err != nil {
			return "", err
		}
	}
	if prompt == "" {
		return "", nil
	}
	user, shared, err := h.scheduleJobAssistantUser(job, job.AssistantModel)
	if err != nil {
		return "", err
	}
	model := strings.TrimSpace(job.AssistantModel)
	if shared != nil {
		// On the shared key a run only uses the models the administrator
		// allows, and never silently switches to another one.
		allowed, ok := assistantSponsoredModelFor(shared, scheduleSharedRequestedModel(model))
		if !ok {
			return "", fmt.Errorf("定时任务使用平台共享 Key 执行，只能用平台管理员允许的模型（%s），任务指定的 %s 不在其中，本次没有运行；请在定时任务里改选允许的模型，或在个人配置中填写自己的 Key", strings.Join(shared, "、"), model)
		}
		model = allowed
	}
	task := assistantTask{
		Name:          job.Name,
		Prompt:        prompt,
		Model:         model,
		ProjectID:     job.ProjectID,
		Notifications: domain.CloneNotificationBindings(job.Notifications),
	}
	ctx := withAssistantExecMode(context.Background(), assistantExecModeForApproval(AssistantApprovalAuto))
	ctx = withAssistantSessionOrigin(ctx, domain.AssistantSourceScheduled, job.ID)
	if err := h.executeAssistantTask(ctx, user, &task); err != nil {
		if msg := strings.TrimSpace(task.LastError); msg != "" {
			return task.LastMessage, fmt.Errorf("%s", msg)
		}
		if errors.Is(err, context.Canceled) {
			return task.LastMessage, fmt.Errorf("生成已停止")
		}
		return task.LastMessage, err
	}
	return firstNonEmpty(task.NotificationResult, task.LastMessage), nil
}

// scheduleJobAssistantUser picks who runs the job: the member the job is bound
// to (whoever last saved it), else the project owner, else (for a non-shared
// job) its owner. A bound job never falls back to someone else's account. The
// model's CLI decides which key they need: their own, else for CodeBuddy the
// platform's shared key, whose allowed models are returned as shared (nil on
// the user's own key).
func (h *assistantHandlers) scheduleJobAssistantUser(job domain.ScheduleJob, model string) (domain.User, []string, error) {
	engine, _ := assistantModelEngine(model)
	if job.RunAsUserID > 0 {
		return h.scheduleJobRunAsUser(job, engine)
	}
	var candidates []domain.User
	add := func(id int64) {
		if id == 0 || h.repo == nil {
			return
		}
		if user, ok := h.repo.User(id); ok {
			candidates = append(candidates, user)
		}
	}
	if project, ok := h.repo.Project(job.ProjectID); ok {
		add(project.OwnerID)
	}
	if job.SharedTaskID == "" {
		add(job.OwnerUserID)
	}
	// Personal keys go first in the original order, so a job whose recorded
	// owner still has a key keeps running on it after the project changes
	// owner; the shared key is only the fallback.
	for _, user := range candidates {
		if key, _ := assistantEngineCredential(user, engine); key != "" {
			return user, nil, nil
		}
	}
	for _, user := range candidates {
		if user, shared, ok := scheduleKeyUser(asScheduleKeyStore(h.repo), user, engine); ok {
			return user, shared, nil
		}
	}
	return domain.User{}, nil, fmt.Errorf("执行提示词需要项目 Owner 的 %s API Key：请在该账号的个人配置中配置，或在自己的个人配置里填好 Key 后编辑并保存一次任务，改用你的 Key", assistantEngineLabel(engine))
}

// scheduleJobRunAsUser runs a bound job as that member while they can still
// write to the project.
func (h *assistantHandlers) scheduleJobRunAsUser(job domain.ScheduleJob, engine string) (domain.User, []string, error) {
	name := firstNonEmpty(job.RunAsName, fmt.Sprintf("鐢ㄦ埛 %d", job.RunAsUserID))
	user, ok := h.repo.User(job.RunAsUserID)
	if !ok {
		return domain.User{}, nil, fmt.Errorf("浠诲姟淇濆瓨浜?%s 鐨勮处鍙峰凡涓嶅瓨鍦細璇风紪杈戝苟淇濆瓨涓€娆′换鍔★紝鏀圭敤浣犵殑 Key", name)
	}
	if !scheduleProjectWriter(h.repo, job.ProjectID, user) {
		return domain.User{}, nil, fmt.Errorf("%s 宸蹭笉鏄椤圭洰鐨勫彲鍐欐垚鍛橈紝涓嶈兘鍐嶇敤鍏?Key 鎵ц锛氳缂栬緫骞朵繚瀛樹竴娆′换鍔★紝鏀圭敤浣犵殑 Key", name)
	}
	runUser, shared, ok := scheduleKeyUser(asScheduleKeyStore(h.repo), user, engine)
	if !ok {
		if engine != assistantEngineCodeBuddy {
			return domain.User{}, nil, fmt.Errorf("鎵ц鎻愮ず璇嶉渶瑕?%s 鐨?%s API Key锛氳鍦ㄨ璐﹀彿鐨勪釜浜洪厤缃腑閰嶇疆", name, assistantEngineLabel(engine))
		}
		return domain.User{}, nil, fmt.Errorf("鎵ц鎻愮ず璇嶉渶瑕?%s 鐨?CodeBuddy API Key锛屽钩鍙颁篃娌℃湁閰嶇疆鍏变韩 Key锛氳鍦ㄨ璐﹀彿鐨勪釜浜洪厤缃腑閰嶇疆锛屾垨鑱旂郴骞冲彴绠＄悊鍛橀厤缃叡浜?Key", name)
	}
	return runUser, shared, nil
}

// scheduleKeyUser is user with the key a scheduled run for engine uses: their
// own, else a platform shared key (whose allowed models are returned as
// shared). OpenCode borrows the shared OpenCode credential, CodeBuddy the
// shared CodeBuddy key; every other CLI has no shared fallback. Everything
// but the key stays the user's own. ok is false when neither exists. The
// implementation lives in schedule_key_user.go.

// scheduleProjectWriter mirrors scheduleProjectAllowed for a stored user.
func scheduleProjectWriter(r interface {
	Project(string) (domain.Project, bool)
	IsPlatformAdmin(string) bool
}, projectID string, user domain.User) bool {
	if r.IsPlatformAdmin(user.LoginName) {
		return true
	}
	project, ok := r.Project(projectID)
	if !ok {
		return false
	}
	if project.OwnerID == user.ID {
		return true
	}
	for _, member := range project.Members {
		if member.UserID == user.ID && !strings.EqualFold(member.Role, "viewer") {
			return true
		}
	}
	return false
}