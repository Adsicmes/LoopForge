package domain

import "testing"

func TestParseCodeupRepoURL(t *testing.T) {
	const want = "codeup:60de7a6852743a5162b5f957/group/demo"
	cases := []struct {
		raw     string
		project string
		org     string
		name    string
		clone   string
	}{
		{raw: "https://codeup.aliyun.com/60de7a6852743a5162b5f957/group/demo.git", project: want, org: "60de7a6852743a5162b5f957", name: "demo", clone: "https://codeup.aliyun.com/60de7a6852743a5162b5f957/group/demo.git"},
		{raw: "https://codeup.aliyun.com/60de7a6852743a5162b5f957/group/demo", project: want, org: "60de7a6852743a5162b5f957", name: "demo", clone: "https://codeup.aliyun.com/60de7a6852743a5162b5f957/group/demo.git"},
		{raw: "https://codeup.aliyun.com/60de7a6852743a5162b5f957/group/demo/tree/master/src", project: want, org: "60de7a6852743a5162b5f957", name: "demo", clone: "https://codeup.aliyun.com/60de7a6852743a5162b5f957/group/demo.git"},
		{raw: "https://codeup.aliyun.com/60de7a6852743a5162b5f957/group/demo/change/12", project: want, org: "60de7a6852743a5162b5f957", name: "demo", clone: "https://codeup.aliyun.com/60de7a6852743a5162b5f957/group/demo.git"},
		{raw: "git clone git@codeup.aliyun.com:60de7a6852743a5162b5f957/group/demo.git", project: want, org: "60de7a6852743a5162b5f957", name: "demo", clone: "https://codeup.aliyun.com/60de7a6852743a5162b5f957/group/demo.git"},
		{raw: "ssh://git@codeup.aliyun.com/60de7a6852743a5162b5f957/group/demo.git", project: want, org: "60de7a6852743a5162b5f957", name: "demo", clone: "https://codeup.aliyun.com/60de7a6852743a5162b5f957/group/demo.git"},
		{raw: "codeup:60de7a6852743a5162b5f957/group/demo", project: want, org: "60de7a6852743a5162b5f957", name: "demo", clone: "https://codeup.aliyun.com/60de7a6852743a5162b5f957/group/demo.git"},
		{raw: "  60de7a6852743a5162b5f957/group/demo  ", project: want, org: "60de7a6852743a5162b5f957", name: "demo", clone: "https://codeup.aliyun.com/60de7a6852743a5162b5f957/group/demo.git"},
	}
	for _, tc := range cases {
		info, ok := ParseCodeupRepoURL(tc.raw)
		if !ok {
			t.Fatalf("%q should parse", tc.raw)
		}
		if info.Project != tc.project || info.OrganizationID != tc.org || info.Name != tc.name || info.CloneURL != tc.clone {
			t.Fatalf("%q => %+v", tc.raw, info)
		}
	}
}

func TestParseCodeupRepoURLRejectsOtherHosts(t *testing.T) {
	for _, raw := range []string{
		"github.com/example/repo",
		"https://github.com/example/repo",
		"https://gitlab.com/group/repo.git",
		"https://codeup.aliyun.com/onlyone",
		"https://codeup.aliyun.com/",
		"",
		"https://codeup.aliyun.com/org/repo%2Fevil",
	} {
		if info, ok := ParseCodeupRepoURL(raw); ok {
			t.Fatalf("%q should not parse, got %+v", raw, info)
		}
	}
}

func TestParseRepositoryURLDispatchesCodeup(t *testing.T) {
	info, ok := ParseRepositoryURL("https://codeup.aliyun.com/60de7a6852743a5162b5f957/group/demo.git")
	if !ok || info.Project != "codeup:60de7a6852743a5162b5f957/group/demo" {
		t.Fatalf("dispatch => %+v ok=%v", info, ok)
	}
}

func TestCodeupRepositoryPath(t *testing.T) {
	if got := CodeupRepositoryPath("codeup:60de7a6852743a5162b5f957/group/demo"); got != "60de7a6852743a5162b5f957/group/demo" {
		t.Fatalf("path = %q", got)
	}
	if got := CodeupRepositoryPath("  "); got != "" {
		t.Fatalf("blank path = %q", got)
	}
}
