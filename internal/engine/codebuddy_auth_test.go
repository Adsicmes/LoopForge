package engine

import (
	"strings"
	"testing"

	"agentflow/internal/domain"
	"agentflow/internal/repo"
)

// CodeBuddy has no platform-shared credential: only the responsible user's own
// key is used, and a user without one resolves to an empty credential.
func TestResolveCodeBuddyCredentialUsesOnlyPersonalKey(t *testing.T) {
	s, err := repo.OpenTest()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	if err := s.PutUser(domain.User{ID: 21, LoginName: "with-key", CodeBuddyAPIKey: "ck_user", CodeBuddyInternetEnvironment: "private-net"}); err != nil {
		t.Fatal(err)
	}
	if err := s.PutUser(domain.User{ID: 22, LoginName: "no-key"}); err != nil {
		t.Fatal(err)
	}
	if got := resolveCodeBuddyCredential(s, 21); got.APIKey != "ck_user" || got.InternetEnvironment != "private-net" {
		t.Fatalf("personal key = %+v", got)
	}
	if got := resolveCodeBuddyCredential(s, 22); got.APIKey != "" {
		t.Fatalf("a user without a key borrowed %+v", got)
	}
	if got := resolveCodeBuddyCredential(nil, 21); got.APIKey != "" {
		t.Fatalf("nil store = %+v", got)
	}
}

func TestSOPExecutorDefaultsToOpenCode(t *testing.T) {
	executors := repo.BuiltInExecutors()
	if len(executors) == 0 || executors[0].ID != "opencode" {
		t.Fatalf("first built-in executor = %+v", executors)
	}
	if strings.TrimSpace(executors[0].Binary) == "" {
		t.Fatalf("OpenCode executor has no binary: %+v", executors[0])
	}
}
