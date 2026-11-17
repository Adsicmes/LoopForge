package httpapi

import (
	"agentflow/internal/domain"
	"agentflow/internal/repo"
)

// testOpenCodeModel is the personal OpenCode model test fixtures use.
const testOpenCodeModel = "opencode:glm-5"

// testOpenCode adds a personal OpenCode credential to a user. Tests that go
// through an internal pipeline (scheduled tasks, clarifications, reports,
// bots, evals) need one because the platform no longer lends CodeBuddy keys.
func testOpenCode(user domain.User, models ...string) domain.User {
	if len(models) == 0 {
		models = []string{"glm-5"}
	}
	if user.AgentCLICredentials == nil {
		user.AgentCLICredentials = map[string]domain.AgentCLICredential{}
	}
	credential := user.AgentCLICredentials["opencode"]
	credential.APIKey = "sk-test-opencode"
	credential.APIKeyConfigured = true
	if credential.BaseURL == "" {
		credential.BaseURL = "https://opencode.example/v1"
	}
	if credential.Protocol == "" {
		credential.Protocol = "chat"
	}
	credential.Models = nil
	for _, id := range models {
		credential.Models = append(credential.Models, domain.CLIModel{ID: id, Name: id})
	}
	user.AgentCLICredentials["opencode"] = credential
	return user
}

// testOpenCodeCredentialUpdate is the credentials-API form of testOpenCode.
func testOpenCodeCredentialUpdate(models ...string) repo.UserCredentialUpdate {
	if len(models) == 0 {
		models = []string{"glm-5"}
	}
	var listed []domain.CLIModel
	for _, id := range models {
		listed = append(listed, domain.CLIModel{ID: id, Name: id})
	}
	return repo.UserCredentialUpdate{AgentCLICredentials: map[string]repo.AgentCLICredentialUpdate{
		"opencode": {APIKey: "sk-test-opencode", Models: &listed},
	}}
}
