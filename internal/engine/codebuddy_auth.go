package engine

import (
	"slices"
	"strings"

	"agentflow/internal/domain"
	"agentflow/internal/repo"
	"agentflow/internal/runtime"
)

const (
	codeBuddyAPIKeyEnv   = "CODEBUDDY_API_KEY"
	codeBuddyInternetEnv = "CODEBUDDY_INTERNET_ENVIRONMENT"
)

// resolveCodeBuddyCredential returns the responsible user's own key. CodeBuddy
// has no platform-shared credential any more: a run without the user's own key
// must fail instead of borrowing someone else's.
func resolveCodeBuddyCredential(s *repo.Store, userID int64) runtime.CodeBuddyCredential {
	if s == nil || userID <= 0 {
		return runtime.CodeBuddyCredential{}
	}
	user, ok := s.User(userID)
	if !ok || strings.TrimSpace(user.CodeBuddyAPIKey) == "" {
		return runtime.CodeBuddyCredential{}
	}
	return runtime.CodeBuddyCredential{APIKey: user.CodeBuddyAPIKey, InternetEnvironment: user.CodeBuddyInternetEnvironment}
}

func hasRunEvent(run domain.Run, typ string) bool {
	return slices.ContainsFunc(run.Events, func(event domain.RunEvent) bool { return event.Type == typ })
}
