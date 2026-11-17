package httpapi

import (
	"context"
	"strings"

	"agentflow/internal/domain"
)

// assistantChatUser returns the identity a CodeBuddy chat turn runs as.
// CodeBuddy no longer has a platform-shared credential: a turn without the
// user's own key is refused instead of borrowing someone else's.
func (h *assistantHandlers) assistantChatUser(user domain.User) (chatUser domain.User, shared []string, ok bool) {
	if strings.TrimSpace(user.CodeBuddyAPIKey) == "" {
		return user, nil, false
	}
	return user, nil, true
}

// chatCatalog is the CodeBuddy catalog of a chat user, in the user's own
// network environment.
func (h *assistantHandlers) chatCatalog(ctx context.Context, user domain.User) assistantModelCatalog {
	return assistantChatCatalog(ctx, user.CodeBuddyInternetEnvironment)
}
