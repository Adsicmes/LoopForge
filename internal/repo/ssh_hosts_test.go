package repo

import (
	"crypto/ed25519"
	"crypto/rand"
	"strings"
	"sync"
	"testing"

	"agentflow/internal/gitwork"
	"golang.org/x/crypto/ssh"
)

func TestSSHHostsStore(t *testing.T) {
	s, err := OpenTest()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	c := gitwork.KnownHostCandidate{Host: "Git.Example", Port: 22, PublicKey: strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key)))}
	if err := s.PutSSHHosts([]gitwork.KnownHostCandidate{c, c}); err != nil {
		t.Fatal(err)
	}
	c.Marker = "revoked"
	if err := s.PutSSHHosts([]gitwork.KnownHostCandidate{c}); err != nil {
		t.Fatal(err)
	}
	trust, err := s.SSHHostSnapshot([]string{"git@git.example:group/repo.git"})
	if err != nil || !strings.Contains(string(trust), "@revoked git.example ") || !strings.Contains(string(trust), "\ngit.example ") && !strings.HasPrefix(string(trust), "git.example ") {
		t.Fatalf("trust=%q err=%v", trust, err)
	}
	if _, err := s.SSHHostSnapshot([]string{"ssh://git@git.example:2222/group/repo.git"}); err == nil {
		t.Fatal("different port trusted")
	}
	ca := c
	ca.Port = 2222
	ca.Marker = "cert-authority"
	if err := s.PutSSHHosts([]gitwork.KnownHostCandidate{ca}); err != nil {
		t.Fatal(err)
	}
	portTrust, err := s.SSHHostSnapshot([]string{"ssh://git@git.example:2222/group/repo.git"})
	if err != nil || !strings.Contains(string(portTrust), "@cert-authority [git.example]:2222 ") || strings.Contains(string(portTrust), "@revoked") {
		t.Fatalf("port trust=%q err=%v", portTrust, err)
	}
	all, err := s.SSHHosts()
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range all {
		if row.Port == 2222 {
			if err := s.DeleteSSHHost(row.ID); err != nil {
				t.Fatal(err)
			}
		}
	}
	rows, err := s.SSHHosts()
	if err != nil || len(rows) != 2 || rows[0].Host != "git.example" {
		t.Fatalf("rows=%+v err=%v", rows, err)
	}
	if err := s.DeleteSSHHost(rows[0].ID); err != nil {
		t.Fatal(err)
	}
	rows, err = s.SSHHosts()
	if err != nil || len(rows) != 1 {
		t.Fatalf("rows=%+v err=%v", rows, err)
	}
}

func TestSSHHostsConcurrentDedupAndReadFailure(t *testing.T) {
	s, err := OpenTest()
	if err != nil {
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
	item := gitwork.KnownHostCandidate{Host: "example.com", Port: 22, PublicKey: strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key)))}
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- s.PutSSHHosts([]gitwork.KnownHostCandidate{item}) }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	rows, err := s.SSHHosts()
	if err != nil || len(rows) != 1 {
		t.Fatalf("rows=%+v err=%v", rows, err)
	}
	s.Close()
	if _, err := s.SSHHostSnapshot([]string{"git@example.com:group/repo"}); err == nil {
		t.Fatal("closed database did not fail closed")
	}
}

func TestSSHHostsKeyRotation(t *testing.T) {
	s, err := OpenTest()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var items []gitwork.KnownHostCandidate
	for i := 0; i < 2; i++ {
		public, _, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		key, err := ssh.NewPublicKey(public)
		if err != nil {
			t.Fatal(err)
		}
		items = append(items, gitwork.KnownHostCandidate{Host: "rotate.example", Port: 22, PublicKey: strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key)))})
	}
	if err := s.PutSSHHosts(items); err != nil {
		t.Fatal(err)
	}
	rows, err := s.SSHHosts()
	if err != nil || len(rows) != 2 || rows[0].Fingerprint == rows[1].Fingerprint {
		t.Fatalf("rows=%+v err=%v", rows, err)
	}
	snapshot, err := s.SSHHostSnapshot([]string{"git@rotate.example:team/repo"})
	if err != nil || strings.Count(string(snapshot), "rotate.example ") != 2 {
		t.Fatalf("snapshot=%q err=%v", snapshot, err)
	}
	if err := s.DeleteSSHHost(rows[0].ID); err != nil {
		t.Fatal(err)
	}
	updated, err := s.SSHHostSnapshot([]string{"git@rotate.example:team/repo"})
	if err != nil || strings.Count(string(updated), "rotate.example ") != 1 || strings.Count(string(snapshot), "rotate.example ") != 2 {
		t.Fatalf("old=%q new=%q err=%v", snapshot, updated, err)
	}
}

// github.com gets its builtin pins without an administrator entry; revoking
// the builtin ed25519 key alone still leaves the other builtin keys trusted
// (first-wins per host key), and hosts without builtin pins fail closed.
func TestSSHHostSnapshotBuiltinGitHub(t *testing.T) {
	s, err := OpenTest()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	trust, err := s.SSHHostSnapshot([]string{"git@github.com:org/repo.git"})
	if err != nil || !strings.Contains(string(trust), "github.com ssh-ed25519 ") {
		t.Fatalf("builtin github trust=%q err=%v", trust, err)
	}
	fallback, err := s.SSHHostSnapshot([]string{"ssh://git@ssh.github.com:443/org/repo.git"})
	if err != nil || !strings.Contains(string(fallback), "[ssh.github.com]:443 ssh-ed25519 ") {
		t.Fatalf("builtin fallback trust=%q err=%v", fallback, err)
	}
	if _, err := s.SSHHostSnapshot([]string{"git@git.example:team/app.git"}); err == nil {
		t.Fatal("a host without builtin pins must still fail closed")
	}
	_, _, githubKey, _, _, err := ssh.ParseKnownHosts([]byte("github.com ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl"))
	if err != nil {
		t.Fatal(err)
	}
	revoked := gitwork.KnownHostCandidate{Host: "github.com", Port: 22, Marker: "revoked", PublicKey: strings.TrimSpace(string(ssh.MarshalAuthorizedKey(githubKey)))}
	if err := s.PutSSHHosts([]gitwork.KnownHostCandidate{revoked}); err != nil {
		t.Fatal(err)
	}
	trust, err = s.SSHHostSnapshot([]string{"git@github.com:org/repo.git"})
	if err != nil || !strings.Contains(string(trust), "@revoked github.com ssh-ed25519 ") || strings.Count(string(trust), "github.com ssh-ed25519 ") != 1 {
		t.Fatalf("revoked builtin ed25519 key: trust=%q err=%v", trust, err)
	}
}
