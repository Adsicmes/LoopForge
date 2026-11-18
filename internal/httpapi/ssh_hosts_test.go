package httpapi

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"agentflow/internal/domain"
	"agentflow/internal/gitwork"
	"golang.org/x/crypto/ssh"
)

func TestSSHHostsAdminAPI(t *testing.T) {
	f := cvBNewHTTPFixture(t)
	ordinary := domain.User{LoginName: "ssh-host-ordinary"}
	if err := f.s.PutUser(ordinary); err != nil {
		t.Fatal(err)
	}
	ordinary, _ = f.s.UserByLoginName(ordinary.LoginName)
	userFixture := f
	userFixture.user = ordinary
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/platform/ssh-hosts"}, {http.MethodPost, "/api/platform/ssh-hosts"},
		{http.MethodPost, "/api/platform/ssh-hosts/preview"}, {http.MethodPost, "/api/platform/ssh-hosts/scan"},
		{http.MethodDelete, "/api/platform/ssh-hosts/1"},
	} {
		resp := cvBRequest(t, userFixture, tc.method, tc.path, map[string]any{}, true)
		if resp.Code != http.StatusForbidden {
			t.Fatalf("%s %s: %d", tc.method, tc.path, resp.Code)
		}
	}
	if _, err := f.s.GrantPlatformAdmin(f.user.LoginName); err != nil {
		t.Fatal(err)
	}
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	content := "git.example " + strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key)))
	resp := cvBRequest(t, f, http.MethodPost, "/api/platform/ssh-hosts/preview", map[string]string{"content": content}, true)
	var preview struct {
		Items []gitwork.KnownHostImportItem `json:"items"`
	}
	if resp.Code != http.StatusOK || json.Unmarshal(resp.Body.Bytes(), &preview) != nil || len(preview.Items) != 1 || preview.Items[0].Candidate == nil {
		t.Fatalf("preview: %d %s", resp.Code, resp.Body.String())
	}
	rows, err := f.s.SSHHosts()
	if err != nil || len(rows) != 0 {
		t.Fatalf("preview persisted candidates: %+v %v", rows, err)
	}
	resp = cvBRequest(t, f, http.MethodPost, "/api/platform/ssh-hosts", map[string]any{"hosts": []gitwork.KnownHostCandidate{*preview.Items[0].Candidate}}, true)
	if resp.Code != http.StatusOK {
		t.Fatalf("save: %d %s", resp.Code, resp.Body.String())
	}
	rows, err = f.s.SSHHosts()
	if err != nil || len(rows) != 1 {
		t.Fatalf("save rows=%+v err=%v", rows, err)
	}
}
