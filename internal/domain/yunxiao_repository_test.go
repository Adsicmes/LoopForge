package domain

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseYunxiaoRepoURL(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		project string
		repo    string
		web     string
		clone   string
	}{
		{
			name:    "web url",
			input:   "https://codeup.aliyun.com/qyd/lianmeng/kr/web",
			project: "codeup:qyd/lianmeng/kr/web",
			repo:    "web",
			web:     "https://codeup.aliyun.com/qyd/lianmeng/kr/web",
			clone:   "https://codeup.aliyun.com/qyd/lianmeng/kr/web.git",
		},
		{
			name:    "web url git suffix",
			input:   "https://codeup.aliyun.com/qyd/lianmeng/kr/web.git",
			project: "codeup:qyd/lianmeng/kr/web",
			repo:    "web",
			web:     "https://codeup.aliyun.com/qyd/lianmeng/kr/web",
			clone:   "https://codeup.aliyun.com/qyd/lianmeng/kr/web.git",
		},
		{
			name:    "web url trailing slash",
			input:   "https://codeup.aliyun.com/qyd/lianmeng/kr/web/",
			project: "codeup:qyd/lianmeng/kr/web",
			repo:    "web",
			web:     "https://codeup.aliyun.com/qyd/lianmeng/kr/web",
			clone:   "https://codeup.aliyun.com/qyd/lianmeng/kr/web.git",
		},
		{
			name:    "change page",
			input:   "https://codeup.aliyun.com/qyd/lianmeng/kr/web/change/1992",
			project: "codeup:qyd/lianmeng/kr/web",
			repo:    "web",
			web:     "https://codeup.aliyun.com/qyd/lianmeng/kr/web",
			clone:   "https://codeup.aliyun.com/qyd/lianmeng/kr/web.git",
		},
		{
			name:    "tree page",
			input:   "https://codeup.aliyun.com/qyd/lianmeng/kr/web/tree/master",
			project: "codeup:qyd/lianmeng/kr/web",
			repo:    "web",
			web:     "https://codeup.aliyun.com/qyd/lianmeng/kr/web",
			clone:   "https://codeup.aliyun.com/qyd/lianmeng/kr/web.git",
		},
		{
			name:    "blob page",
			input:   "https://codeup.aliyun.com/qyd/lianmeng/kr/web/blob/master/README.md",
			project: "codeup:qyd/lianmeng/kr/web",
			repo:    "web",
			web:     "https://codeup.aliyun.com/qyd/lianmeng/kr/web",
			clone:   "https://codeup.aliyun.com/qyd/lianmeng/kr/web.git",
		},
		{
			name:    "dash subpage",
			input:   "https://codeup.aliyun.com/qyd/lianmeng/kr/web/-/tree/master",
			project: "codeup:qyd/lianmeng/kr/web",
			repo:    "web",
			web:     "https://codeup.aliyun.com/qyd/lianmeng/kr/web",
			clone:   "https://codeup.aliyun.com/qyd/lianmeng/kr/web.git",
		},
		{
			name:    "region host kept",
			input:   "https://acme.devops.aliyuncs.com/qyd/lianmeng/kr/web/change/7",
			project: "codeup:qyd/lianmeng/kr/web",
			repo:    "web",
			web:     "https://acme.devops.aliyuncs.com/qyd/lianmeng/kr/web",
			clone:   "https://acme.devops.aliyuncs.com/qyd/lianmeng/kr/web.git",
		},
		{
			name:    "bare host",
			input:   "codeup.aliyun.com/qyd/lianmeng/kr/web",
			project: "codeup:qyd/lianmeng/kr/web",
			repo:    "web",
			web:     "https://codeup.aliyun.com/qyd/lianmeng/kr/web",
			clone:   "https://codeup.aliyun.com/qyd/lianmeng/kr/web.git",
		},
		{
			name:    "scp ssh keeps ssh clone",
			input:   "git@codeup.aliyun.com:qyd/lianmeng/kr/web.git",
			project: "codeup:qyd/lianmeng/kr/web",
			repo:    "web",
			web:     "https://codeup.aliyun.com/qyd/lianmeng/kr/web",
			clone:   "git@codeup.aliyun.com:qyd/lianmeng/kr/web.git",
		},
		{
			name:    "ssh url with port keeps ssh clone",
			input:   "ssh://git@codeup.aliyun.com:22/qyd/lianmeng/kr/web.git",
			project: "codeup:qyd/lianmeng/kr/web",
			repo:    "web",
			web:     "https://codeup.aliyun.com/qyd/lianmeng/kr/web",
			clone:   "git@codeup.aliyun.com:qyd/lianmeng/kr/web.git",
		},
		{
			name:    "ssh url without port",
			input:   "ssh://git@codeup.aliyun.com/qyd/lianmeng/kr/web.git",
			project: "codeup:qyd/lianmeng/kr/web",
			repo:    "web",
			web:     "https://codeup.aliyun.com/qyd/lianmeng/kr/web",
			clone:   "git@codeup.aliyun.com:qyd/lianmeng/kr/web.git",
		},
		{
			name:    "region ssh",
			input:   "git@acme.devops.aliyuncs.com:qyd/lianmeng/kr/web.git",
			project: "codeup:qyd/lianmeng/kr/web",
			repo:    "web",
			web:     "https://acme.devops.aliyuncs.com/qyd/lianmeng/kr/web",
			clone:   "git@acme.devops.aliyuncs.com:qyd/lianmeng/kr/web.git",
		},
		{
			name:    "explicit prefix",
			input:   "codeup:qyd/lianmeng/kr/web",
			project: "codeup:qyd/lianmeng/kr/web",
			repo:    "web",
			web:     "https://codeup.aliyun.com/qyd/lianmeng/kr/web",
			clone:   "https://codeup.aliyun.com/qyd/lianmeng/kr/web.git",
		},
		{
			name:    "git clone prefix",
			input:   "git clone https://codeup.aliyun.com/qyd/lianmeng/kr/web.git",
			project: "codeup:qyd/lianmeng/kr/web",
			repo:    "web",
			web:     "https://codeup.aliyun.com/qyd/lianmeng/kr/web",
			clone:   "https://codeup.aliyun.com/qyd/lianmeng/kr/web.git",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			info, ok := ParseYunxiaoRepoURL(tc.input)
			if !ok {
				t.Fatalf("ParseYunxiaoRepoURL(%q) rejected", tc.input)
			}
			if info.Provider != RepositoryProviderYunxiao {
				t.Fatalf("provider = %q", info.Provider)
			}
			if info.Project != tc.project || info.Name != tc.repo {
				t.Fatalf("project=%q name=%q, want %q %q", info.Project, info.Name, tc.project, tc.repo)
			}
			if info.WebURL != tc.web || info.CloneURL != tc.clone {
				t.Fatalf("web=%q clone=%q, want %q %q", info.WebURL, info.CloneURL, tc.web, tc.clone)
			}
		})
	}
}

func TestParseYunxiaoRepoURLRejects(t *testing.T) {
	for _, input := range []string{
		"",
		"   ",
		"https://user:secret@codeup.aliyun.com/qyd/lianmeng/kr/web",
		"https://codeup.aliyun.com/qyd/../kr/web",
		"codeup:qyd/../web",
		"https://codeup.aliyun.com/qyd//web",
		"https://gitlab.com/qyd/lianmeng/kr/web",
		"https://codeup.aliyun.com.evil.invalid/qyd/web",
		"https://codeup.aliyun.com/qyd",
		"codeup:",
		"codeup:qyd/%2e%2e/web",
		"https://codeup.aliyun.com/qyd/web foo",
		"git@github.com:qyd/web.git",
		"ssh://deploy@codeup.aliyun.com/qyd/web.git",
		"https://acme.devops.aliyuncs.com/qyd",
	} {
		if info, ok := ParseYunxiaoRepoURL(input); ok {
			t.Fatalf("invalid reference accepted: %q -> %+v", input, info)
		}
	}
}

func TestParseRepositoryURLRoutesCodeup(t *testing.T) {
	got, ok := ParseRepositoryURL("git@codeup.aliyun.com:qyd/lianmeng/kr/web.git")
	if !ok || got.Provider != RepositoryProviderYunxiao || got.Project != "codeup:qyd/lianmeng/kr/web" {
		t.Fatalf("codeup routing = %+v %v", got, ok)
	}
	github, ok := ParseRepositoryURL("https://github.com/Tencent/LoopForge")
	if !ok || github.Provider != "github" || github.Project != "github:Tencent/LoopForge" {
		t.Fatalf("github routing = %+v %v", github, ok)
	}
	gongfeng, ok := ParseRepositoryURL("https://git.example.com/infra/gateway")
	if !ok || gongfeng.Provider != "gongfeng" || gongfeng.Project != "infra/gateway" {
		t.Fatalf("gongfeng routing = %+v %v", gongfeng, ok)
	}
}

func TestIsYunxiaoRemote(t *testing.T) {
	if !IsYunxiaoRemote("codeup:qyd/lianmeng/kr/web") {
		t.Fatal("codeup identity must be recognized")
	}
	for _, project := range []string{"", "github:Tencent/LoopForge", "infra/gateway"} {
		if IsYunxiaoRemote(project) {
			t.Fatalf("%q must not be a yunxiao remote", project)
		}
	}
}

func TestYunxiaoRepositoryJSONFields(t *testing.T) {
	repo := Repository{Provider: RepositoryProviderYunxiao, GongfengProjectID: "codeup:qyd/web", YunxiaoRepoID: 2464348}
	raw, err := json.Marshal(repo)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"yunxiaoRepoId":2464348`) {
		t.Fatalf("repository JSON = %s", raw)
	}
	project := Project{YunxiaoSpaceID: "space", YunxiaoSprintID: "sprint", YunxiaoDefaultAssigneeID: "u1", YunxiaoStatusMap: map[string]string{"DONE": "done"}}
	raw, err = json.Marshal(project)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"yunxiaoSpaceId":"space"`, `"yunxiaoSprintId":"sprint"`, `"yunxiaoDefaultAssigneeId":"u1"`, `"DONE":"done"`} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("project JSON missing %s: %s", want, raw)
		}
	}
}

func TestMRURL(t *testing.T) {
	if got := MRURL("", "infra/gateway", 7); got != "https://git.example.com/infra/gateway/-/merge_requests/7" {
		t.Fatalf("gongfeng URL = %q", got)
	}
	if got := MRURL("", "github:Tencent/LoopForge", 12); got != "https://github.com/Tencent/LoopForge/pull/12" {
		t.Fatalf("github URL = %q", got)
	}
	if got := GongfengMRURL("github:Tencent/LoopForge", 12); got != "https://github.com/Tencent/LoopForge/pull/12" {
		t.Fatalf("wrapper github URL = %q", got)
	}
	want := "https://codeup.aliyun.com/qyd/lianmeng/kr/web/change/1992"
	if got := MRURL(RepositoryProviderYunxiao, "codeup:qyd/lianmeng/kr/web", 1992); got != want {
		t.Fatalf("codeup URL = %q, want %q", got, want)
	}
	if got := GongfengMRURL("codeup:qyd/lianmeng/kr/web", 1992); got != want {
		t.Fatalf("wrapper codeup URL = %q", got)
	}
	if got := MRURL("", "codeup:qyd/web", 3); got != "https://codeup.aliyun.com/qyd/web/change/3" {
		t.Fatalf("codeup URL from identity = %q", got)
	}
	if got := MRURL(RepositoryProviderYunxiao, "codeup:qyd/web", 0); got != "" {
		t.Fatalf("invalid iid must be empty, got %q", got)
	}

	old := YunxiaoWebBase
	YunxiaoWebBase = "https://acme.devops.aliyuncs.com/"
	defer func() { YunxiaoWebBase = old }()
	if got := MRURL(RepositoryProviderYunxiao, "codeup:qyd/web", 5); got != "https://acme.devops.aliyuncs.com/qyd/web/change/5" {
		t.Fatalf("region codeup URL = %q", got)
	}
}

func TestParseMRs(t *testing.T) {
	text := strings.Join([]string{
		"PR https://github.com/Tencent/LoopForge/pull/12",
		"MR https://git.example.com/example/dns/dns-service/-/merge_requests/558",
		"Codeup https://codeup.aliyun.com/qyd/lianmeng/kr/web/change/1992",
		"Region https://acme.devops.aliyuncs.com/qyd/other/change/7",
	}, "\n")
	refs := ParseMRs(text)
	if len(refs) != 4 {
		t.Fatalf("refs = %+v", refs)
	}
	wants := []MRRef{
		{Provider: "github", Project: "github:Tencent/LoopForge", IID: 12, URL: "https://github.com/Tencent/LoopForge/pull/12"},
		{Provider: "gongfeng", Project: "example/dns/dns-service", IID: 558, URL: "https://git.example.com/example/dns/dns-service/-/merge_requests/558"},
		{Provider: RepositoryProviderYunxiao, Project: "codeup:qyd/lianmeng/kr/web", IID: 1992, URL: "https://codeup.aliyun.com/qyd/lianmeng/kr/web/change/1992"},
		{Provider: RepositoryProviderYunxiao, Project: "codeup:qyd/other", IID: 7, URL: "https://acme.devops.aliyuncs.com/qyd/other/change/7"},
	}
	for i, want := range wants {
		if refs[i] != want {
			t.Fatalf("ref[%d] = %+v, want %+v", i, refs[i], want)
		}
	}
}

func TestParseMRsDedupsCodeup(t *testing.T) {
	text := "-https://codeup.aliyun.com/qyd/web/change/1\n+https://codeup.aliyun.com/qyd/web/change/1\nhttps://codeup.aliyun.com/qyd/web/-/change/1"
	refs := ParseMRs(text)
	if len(refs) != 1 || refs[0].Project != "codeup:qyd/web" || refs[0].IID != 1 {
		t.Fatalf("refs = %+v", refs)
	}
	if refs[0].URL != "https://codeup.aliyun.com/qyd/web/change/1" {
		t.Fatalf("url = %q", refs[0].URL)
	}
	// A /change/ URL on a non-Codeup host is not a hosted MR reference.
	if refs := ParseMRs("https://example.com/org/repo/change/9"); len(refs) != 0 {
		t.Fatalf("foreign host accepted: %+v", refs)
	}
}

func TestParseGongfengMRsCompatibility(t *testing.T) {
	refs := ParseGongfengMRs("https://git.example.com/org/repo/merge_requests/282 https://github.com/Tencent/LoopForge/pull/9 https://codeup.aliyun.com/qyd/web/change/3")
	if len(refs) != 3 {
		t.Fatalf("refs = %+v", refs)
	}
	if refs[0].Project != "org/repo" || refs[0].IID != 282 {
		t.Fatalf("gongfeng ref = %+v", refs[0])
	}
	if refs[1].Project != "github:Tencent/LoopForge" || refs[1].IID != 9 {
		t.Fatalf("github ref = %+v", refs[1])
	}
	if refs[2].Project != "codeup:qyd/web" || refs[2].IID != 3 {
		t.Fatalf("codeup ref = %+v", refs[2])
	}
	project, iid, url, ok := ParseGongfengMR("Codeup https://codeup.aliyun.com/qyd/web/change/1992")
	if !ok || project != "codeup:qyd/web" || iid != 1992 || url != "https://codeup.aliyun.com/qyd/web/change/1992" {
		t.Fatalf("ParseGongfengMR codeup = %q %d %q %v", project, iid, url, ok)
	}
}
