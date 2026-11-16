package httpapi

import (
	"net/http"
	"path/filepath"
	"testing"

	"agentflow/internal/domain"
	"agentflow/internal/engine"
	"agentflow/internal/repo"
	"agentflow/internal/runtime"
	"agentflow/internal/tracker"
)

func TestYunxiaoRepositoryRegistration(t *testing.T) {
	store, err := repo.OpenTest()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	handler := New(store, engine.New(store, runtime.NewManager(filepath.Join(t.TempDir(), "workspaces"))), tracker.NewService(), "", "")
	requestJSON[domain.Project](t, handler, http.MethodPost, "/api/projects", domain.Project{ID: "yunxiao-project", Key: "YX", Name: "Yunxiao"}, http.StatusOK)

	webPaste := requestJSON[domain.Repository](t, handler, http.MethodPost, "/api/repositories", map[string]any{
		"projectId": "yunxiao-project",
		"repoUrl":   "https://codeup.aliyun.com/acme/group/service/-/tree/master",
	}, http.StatusOK)
	if webPaste.Provider != domain.RepositoryProviderYunxiao || webPaste.GongfengProjectID != "codeup:acme/group/service" {
		t.Fatalf("web paste = %+v", webPaste)
	}
	if webPaste.CloneURL != "git@codeup.aliyun.com:acme/group/service.git" {
		t.Fatalf("Codeup HTTPS paste must become an SSH clone URL, got %q", webPaste.CloneURL)
	}

	sshPaste := requestJSON[domain.Repository](t, handler, http.MethodPost, "/api/repositories", map[string]any{
		"projectId": "yunxiao-project",
		"repoUrl":   "git@codeup.aliyun.com:acme/group/ssh.git",
	}, http.StatusOK)
	if sshPaste.Provider != domain.RepositoryProviderYunxiao || sshPaste.CloneURL != "git@codeup.aliyun.com:acme/group/ssh.git" {
		t.Fatalf("ssh paste = %+v", sshPaste)
	}

	requestJSON[map[string]string](t, handler, http.MethodPost, "/api/repositories", map[string]any{
		"projectId": "yunxiao-project",
		"repoUrl":   "https://codeup.example.invalid/acme/group/service",
	}, http.StatusBadRequest)
}
