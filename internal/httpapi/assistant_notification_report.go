package httpapi

import (
	"agentflow/internal/domain"
	"agentflow/internal/notification"
	"context"
	"path/filepath"
	"strings"
)

func (h *assistantHandlers) generateSOPReport(ctx context.Context, run domain.Run, model, prompt string) (domain.AINotificationContent, error) {
	return h.generateReport(ctx, run, model, prompt, "你是整体交付报告编写者。只能根据提供的资料总结结果。仅允许调用 submit_notification_report，必须通过该工具提交报告，不能用普通文本代替。")
}
func (h *assistantHandlers) generateReport(ctx context.Context, run domain.Run, model, prompt, system string) (domain.AINotificationContent, error) {
	user, ok := h.repo.User(run.ExecutionContext.ResponsibleUserID)
	credential, credentialOK := openCodeProviderCredential(h.repo, run.ExecutionContext.ResponsibleUserID)
	if !ok || !credentialOK {
		return domain.AINotificationContent{}, notification.ErrReportCredentials
	}
	engineModel := ""
	if value := strings.TrimSpace(model); value != "" && value != "default" {
		engine, selected := assistantModelEngine(value)
		if engine == assistantEngineOpenCode {
			engineModel = selected
		}
		// A legacy CodeBuddy (or other CLI) model is ignored: this pipeline now
		// runs on OpenCode and resolves its default below.
	}
	if engineModel == "" {
		engineModel = openCodeFirstModel(ctx, credential)
		if engineModel == "" {
			return domain.AINotificationContent{}, notification.ErrReportModel
		}
	}
	if credential.Sponsored {
		allowed := false
		for _, item := range credential.Models {
			if item.ID == engineModel {
				allowed = true
				break
			}
		}
		if !allowed {
			return domain.AINotificationContent{}, notification.ErrReportModel
		}
	}
	model = assistantEngineOpenCode + ":" + engineModel
	if user.AgentCLICredentials == nil {
		user.AgentCLICredentials = map[string]domain.AgentCLICredential{}
	}
	user.AgentCLICredentials[assistantEngineOpenCode] = domain.AgentCLICredential{
		APIKey: credential.APIKey, APIKeyConfigured: true, BaseURL: credential.BaseURL, Protocol: credential.Protocol, Models: credential.Models,
	}

	modelCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	if err := ensureAssistantSessionDirs(user.ID); err != nil {
		return domain.AINotificationContent{}, notification.ErrReportCLI
	}
	session, files, err := startAgentToolSession(ctx, user, run.ExecutionContext.ProjectID, AgentToolReport, assistantExecMode{}, "", nil, cancel, filepath.Join(assistantSessionRoot(user.ID), "mcp-reports"))
	if err != nil {
		return domain.AINotificationContent{}, notification.ErrReportCLI
	}
	defer session.Close()
	defer files.Cleanup()
	session.RunID = run.ID
	session.SetTerminalOnly()
	generate := h.generate
	if generate == nil {
		generate = func(callCtx context.Context, input string, u domain.User) (string, error) {
			raw, _, err := runAssistantCLI(callCtx, assistantCLIRequest{Prompt: input, Model: model, UserID: u.ID, ReportOnly: true, Native: true, MCPConfig: files.MCPConfig, Settings: files.Settings, SystemPrompt: system}, u, nil)
			return raw, err
		}
	}
	for attempt := 0; attempt <= nativeTerminalMaxRetries; attempt++ {
		input := prompt
		if attempt > 0 {
			input = "上一轮没有有效提交报告。请修正参数并调用 submit_notification_report；普通文本不会被接收。\n" + prompt
		}
		_, cliErr := generate(modelCtx, input, user)
		if result, ok := session.takeFinal(); ok && result.Report != nil {
			report := *result.Report
			report.Title = redactAssistantSecrets(strings.ReplaceAll(report.Title, user.CodeBuddyAPIKey, "[redacted]"), user)
			report.Summary = redactAssistantSecrets(strings.ReplaceAll(report.Summary, user.CodeBuddyAPIKey, "[redacted]"), user)
			report.Body = redactAssistantSecrets(strings.ReplaceAll(report.Body, user.CodeBuddyAPIKey, "[redacted]"), user)
			return report, nil
		}
		if ctx.Err() != nil {
			return domain.AINotificationContent{}, ctx.Err()
		}
		if cliErr != nil && !nativeShouldRetryTerminal(ctx, cliErr) {
			return domain.AINotificationContent{}, notification.ErrReportCLI
		}
	}
	return domain.AINotificationContent{}, notification.ErrReportFormat
}
