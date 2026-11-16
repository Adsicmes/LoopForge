package repo

import "strings"

// YunxiaoCredential returns the user's Yunxiao personal access token. Unlike
// GitCredential it is used only for OpenAPI calls, never as a Git transport
// password: Codeup repositories are cloned and pushed over SSH in the MVP.
func (r *UsersRepo) YunxiaoCredential(id int64) string {
	user, ok := r.User(id)
	if !ok {
		return ""
	}
	return strings.TrimSpace(user.YunxiaoToken)
}
