package httpapi

import (
	"context"
	"os"
	"strings"
	"time"

	"agentflow/internal/codeup"
)

// codeupAPIBaseEnv is the deployment override for the Yunxiao OpenAPI host.
// Region editions (for example openapi.cn-hangzhou.aliyuncs.com) set it once.
const codeupAPIBaseEnv = "AGENTFLOW_CODEUP_API_BASE"

// verifyCodeupToken validates an Aliyun Yunxiao personal access token by
// asking the OpenAPI who it belongs to. The organization id is not needed:
// /platform/user is organization-independent, and per-repository permissions
// are enforced later, when the token is actually used.
func verifyCodeupToken(ctx context.Context, token string) gitTokenVerifyResult {
	client, err := codeup.New(codeup.Config{
		APIBase: strings.TrimSpace(os.Getenv(codeupAPIBaseEnv)),
		Token:   token,
	})
	if err != nil {
		return gitTokenVerifyResult{Error: err.Error()}
	}
	verifyCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	user, err := client.GetUser(verifyCtx)
	if err != nil {
		return gitTokenVerifyResult{Error: err.Error()}
	}
	login := strings.TrimSpace(user.Username)
	if login == "" {
		login = strings.TrimSpace(user.Name)
	}
	// Yunxiao does not report token scopes; a token that resolves to a user is
	// valid, and repository access is checked on the first real API call.
	return gitTokenVerifyResult{Valid: true, Login: login}
}
