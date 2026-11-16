package domain

import "testing"

func TestRepositoryUsesSSH(t *testing.T) {
	cases := []struct {
		provider string
		clone    string
		want     bool
	}{
		{RepositoryProviderSSH, "git@gitlab.example:g/r.git", true},
		{RepositoryProviderYunxiao, "git@codeup.aliyun.com:org/repo.git", true},
		{RepositoryProviderYunxiao, "ssh://git@codeup.aliyun.com/org/repo.git", true},
		{RepositoryProviderYunxiao, "https://codeup.aliyun.com/org/repo.git", false},
		{"github", "git@github.com:o/r.git", false},
		{"gongfeng", "git@git.example.com:g/r.git", false},
	}
	for _, tc := range cases {
		if got := RepositoryUsesSSH(tc.provider, tc.clone); got != tc.want {
			t.Fatalf("RepositoryUsesSSH(%q, %q) = %v, want %v", tc.provider, tc.clone, got, tc.want)
		}
	}
}
