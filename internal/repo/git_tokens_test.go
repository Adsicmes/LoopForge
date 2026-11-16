package repo

import (
	"testing"

	"agentflow/internal/domain"
)

// A user may hold one token per hosting platform at the same time: cloning a
// Codeup repository must not borrow or clobber the GitHub token.
func TestGitTokensKeepOnePerPlatform(t *testing.T) {
	st, err := OpenTest()
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.PutUser(domain.User{LoginName: "multi-git-owner"}); err != nil {
		t.Fatal(err)
	}
	user, ok := st.UserByLoginName("multi-git-owner")
	if !ok {
		t.Fatal("missing user")
	}

	if _, err := st.UpdateUserCredentials(user.ID, UserCredentialUpdate{GitToken: "ghp_github", GitTokenProvider: "github"}); err != nil {
		t.Fatal(err)
	}
	updated, err := st.UpdateUserCredentials(user.ID, UserCredentialUpdate{GitToken: "codeup-pat", GitTokenProvider: "codeup"})
	if err != nil {
		t.Fatal(err)
	}
	if got := updated.GitTokenFor(domain.GitProviderGitHub); got != "ghp_github" {
		t.Fatalf("github token = %q, want ghp_github", got)
	}
	if got := updated.GitTokenFor(domain.GitProviderCodeup); got != "codeup-pat" {
		t.Fatalf("codeup token = %q, want codeup-pat", got)
	}
	// The legacy column keeps mirroring GitHub so an older reader still works.
	if updated.GitToken != "ghp_github" {
		t.Fatalf("legacy slot = %q, want the github token", updated.GitToken)
	}

	// Re-reading proves the map survives the round trip through user_configs.
	reread, ok := st.User(user.ID)
	if !ok {
		t.Fatal("missing user after write")
	}
	if got := reread.GitTokenFor(domain.GitProviderCodeup); got != "codeup-pat" {
		t.Fatalf("reloaded codeup token = %q", got)
	}

	// Clearing one platform leaves the other in place.
	cleared, err := st.UpdateUserCredentials(user.ID, UserCredentialUpdate{GitTokenClear: true, GitTokenProvider: "codeup"})
	if err != nil {
		t.Fatal(err)
	}
	if got := cleared.GitTokenFor(domain.GitProviderCodeup); got != "" {
		t.Fatalf("codeup token still set: %q", got)
	}
	if got := cleared.GitTokenFor(domain.GitProviderGitHub); got != "ghp_github" {
		t.Fatalf("github token was cleared with codeup: %q", got)
	}
	providers := cleared.GitTokenProvidersOf()
	if len(providers) != 1 || providers[0] != domain.GitProviderGitHub {
		t.Fatalf("providers = %v, want [github]", providers)
	}
}

// Rows written before tokens were kept per platform must keep working, and an
// unrecognisable legacy token is still handed out so nothing regresses.
func TestGitTokensLegacyRowStillResolves(t *testing.T) {
	github := domain.User{GitToken: "ghp_legacy"}
	if got := github.GitTokenFor(domain.GitProviderGitHub); got != "ghp_legacy" {
		t.Fatalf("github = %q", got)
	}
	if got := github.GitTokenFor(domain.GitProviderCodeup); got != "" {
		t.Fatalf("a github-prefixed legacy token leaked to codeup: %q", got)
	}

	gongfeng := domain.User{GitToken: "gf-oauth", GongfengRefreshToken: "refresh"}
	if got := gongfeng.GitTokenFor(domain.GitProviderGongfeng); got != "gf-oauth" {
		t.Fatalf("gongfeng = %q", got)
	}

	unknown := domain.User{GitToken: "opaque-token"}
	for _, provider := range []string{domain.GitProviderGitHub, domain.GitProviderCodeup} {
		if got := unknown.GitTokenFor(provider); got != "opaque-token" {
			t.Fatalf("opaque legacy token not returned for %s: %q", provider, got)
		}
	}

	// Once a row stores a map, the legacy column is no longer a stand-in.
	migrated := domain.User{GitToken: "ghp_github", GitTokens: map[string]string{domain.GitProviderCodeup: "codeup-pat"}}
	if got := migrated.GitTokenFor(domain.GitProviderCodeup); got != "codeup-pat" {
		t.Fatalf("codeup = %q", got)
	}
}

func TestGitProviderForRepository(t *testing.T) {
	cases := []struct {
		repository domain.Repository
		want       string
	}{
		{domain.Repository{Provider: "codeup"}, domain.GitProviderCodeup},
		{domain.Repository{CloneURL: "https://codeup.aliyun.com/60de/group/demo.git"}, domain.GitProviderCodeup},
		{domain.Repository{CloneURL: "git@github.com:owner/demo.git"}, domain.GitProviderGitHub},
		{domain.Repository{CloneURL: "https://git.code.tencent.com/project/demo.git"}, domain.GitProviderGongfeng},
		{domain.Repository{CloneURL: "https://example.com/owner/demo.git"}, ""},
	}
	for _, item := range cases {
		if got := domain.GitProviderForRepository(item.repository); got != item.want {
			t.Fatalf("%s: got %q want %q", item.repository.CloneURL+item.repository.Provider, got, item.want)
		}
	}
	context := domain.RepositoryContext{CloneURL: "https://codeup.aliyun.com/60de/group/demo.git"}
	if got := domain.GitProviderForRepositoryContext(context); got != domain.GitProviderCodeup {
		t.Fatalf("repository context = %q", got)
	}
}

// PublicUser tells the settings panel which platforms hold a token without
// ever serialising one.
func TestPublicUserListsGitTokenPlatforms(t *testing.T) {
	user := domain.PublicUser(domain.User{
		GitTokens: map[string]string{
			domain.GitProviderGitHub: "ghp_github",
			domain.GitProviderCodeup: "codeup-pat",
		},
	})
	if !user.GitTokenConfigured {
		t.Fatal("configured flag not set")
	}
	if len(user.GitTokenProviders) != 2 {
		t.Fatalf("providers = %v", user.GitTokenProviders)
	}
	if user.GitTokens != nil || user.GitToken != "" {
		t.Fatal("public user leaked a token")
	}
}
