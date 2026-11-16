package domain

import (
	"net/url"
	"regexp"
	"strings"
)

// CodeupHost is the Aliyun Yunxiao (云效) Codeup git host. Codeup repositories
// are addressed as {organizationId}/{group?}/{repository}; the organization
// id is part of the git path, not a separate tenant URL.
const CodeupHost = "codeup.aliyun.com"

// CodeupProvider is the repository provider for Codeup-hosted repositories.
// Git transport is plain HTTPS with the user's personal access token, while
// merge requests, work items and repository discovery go through the Yunxiao
// OpenAPI (openapi-rdc.aliyuncs.com).
const CodeupProvider = "codeup"

// CodeupRepoInfo is the identity parsed out of one pasted Codeup reference.
type CodeupRepoInfo struct {
	// Project is the LoopForge repository identity: "codeup:" + path with the
	// organization namespace, e.g. codeup:60de7a6852743a5162b5f957/group/demo.
	Project string
	// Path is the path with namespace, without the "codeup:" prefix. The
	// Yunxiao OpenAPI accepts it URL-encoded as the repository identity.
	Path string
	// OrganizationID is the first path segment.
	OrganizationID string
	// Name is the last path segment.
	Name string
	// WebURL and CloneURL are credential-free.
	WebURL   string
	CloneURL string
}

// codeupOrganizationPattern matches a Yunxiao organization id: 24 lowercase
// hex characters. It is what makes a bare org/repo path unambiguous.
var codeupOrganizationPattern = regexp.MustCompile(`^[0-9a-f]{24}$`)

// codeupSubpages are URL suffixes that point at a sub page of a repository,
// never at the repository itself.
var codeupSubpages = []string{"/tree/", "/blob/", "/merge_request", "/change/", "/commits", "/branches", "/tags", "/settings", "/issues", "/pipelines", "/webhook"}

// ParseCodeupRepoURL accepts the reference forms a Codeup user actually
// copies: HTTPS clone URL, SSH clone URL, "git clone ..." line, the
// https://codeup.aliyun.com/{org}/{group}/{repo} web URL, and the bare
// {org}/{group}/{repo} path or the "codeup:" shorthand.
func ParseCodeupRepoURL(raw string) (CodeupRepoInfo, bool) {
	s := strings.Trim(strings.TrimSpace(raw), `"'`)
	if len(s) >= 10 && strings.EqualFold(s[:10], "git clone ") {
		s = strings.Trim(strings.TrimSpace(s[10:]), `"'`)
		if i := strings.IndexAny(s, " \t"); i >= 0 {
			s = s[:i]
		}
	}
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "codeup:") {
		s = "https://" + CodeupHost + "/" + strings.TrimPrefix(s, "codeup:")
	}
	if s == "" || strings.ContainsAny(s, " \t\"'<>()|") {
		return CodeupRepoInfo{}, false
	}
	s = strings.TrimSuffix(s, "/")
	// Converge ssh / https / scp forms on host+path.
	pathSource := s
	if i := strings.Index(pathSource, "://"); i >= 0 {
		pathSource = pathSource[i+3:]
	}
	if i := strings.LastIndex(pathSource, "@"); i >= 0 {
		pathSource = pathSource[i+1:]
	}
	// Drop query and fragment before splitting the path.
	if i := strings.IndexAny(pathSource, "?#"); i >= 0 {
		pathSource = pathSource[:i]
	}
	path := ""
	if strings.HasPrefix(pathSource, CodeupHost) {
		path = pathSource[len(CodeupHost):]
		path = strings.TrimPrefix(path, ":") // scp form: codeup.aliyun.com:org/repo.git
		path = strings.TrimPrefix(path, "/")
	} else if !strings.ContainsAny(pathSource, ":@") {
		// Bare path with no host, e.g. 60de7a6852743a5162b5f957/group/demo.
		// Only an organization id in the Yunxiao form is accepted here, so a
		// pasted github.com/group/repo can never be mistaken for Codeup.
		first := pathSource
		if i := strings.Index(first, "/"); i > 0 {
			first = first[:i]
		}
		if !codeupOrganizationPattern.MatchString(first) {
			return CodeupRepoInfo{}, false
		}
		path = pathSource
	} else {
		return CodeupRepoInfo{}, false
	}
	path = strings.TrimSuffix(path, ".git")
	path = strings.Trim(path, "/")
	if path == "" {
		return CodeupRepoInfo{}, false
	}
	for _, marker := range codeupSubpages {
		if i := strings.Index(path, marker); i > 0 {
			path = path[:i]
			break
		}
	}
	segments := strings.Split(path, "/")
	if len(segments) < 2 {
		// A repository always lives inside an organization.
		return CodeupRepoInfo{}, false
	}
	for _, segment := range segments {
		if segment == "" || segment == "." || segment == ".." {
			return CodeupRepoInfo{}, false
		}
		if u, err := url.PathUnescape(segment); err != nil || u != segment {
			return CodeupRepoInfo{}, false
		}
	}
	info := CodeupRepoInfo{
		Path:           path,
		OrganizationID: segments[0],
		Name:           segments[len(segments)-1],
	}
	info.Project = CodeupProvider + ":" + path
	info.WebURL = "https://" + CodeupHost + "/" + path
	info.CloneURL = info.WebURL + ".git"
	return info, true
}

// CodeupRepositoryPath strips the "codeup:" prefix from a repository identity.
func CodeupRepositoryPath(project string) string {
	return strings.TrimPrefix(strings.TrimSpace(project), CodeupProvider+":")
}
