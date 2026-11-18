package gitwork

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

func TestParseKnownHostsImport(t *testing.T) {
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	encoded := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key)))
	input := fmt.Sprintf("# comment\nGit.EXAMPLE,[git.example]:2222,*.example %s\n@revoked git.example %s\n@cert-authority [2001:db8::1]:2222 %s\n|1|hash|hash %s\ninvalid\n", encoded, encoded, encoded, encoded)
	items, err := ParseKnownHostsImport([]byte(input))
	if err != nil || len(items) != 7 {
		t.Fatalf("items=%+v err=%v", items, err)
	}
	if c := items[0].Candidate; c == nil || c.Host != "git.example" || c.Port != 22 || c.Fingerprint != ssh.FingerprintSHA256(key) {
		t.Fatalf("first candidate: %+v", items[0])
	}
	if c := items[1].Candidate; c == nil || c.Port != 2222 {
		t.Fatalf("custom port: %+v", items[1])
	}
	if items[2].Error == "" || items[5].Error == "" || items[6].Error == "" {
		t.Fatalf("unsupported entries must be reported: %+v", items)
	}
	if items[3].Candidate == nil || items[3].Candidate.Marker != "revoked" || items[4].Candidate == nil || items[4].Candidate.Marker != "cert-authority" || items[4].Candidate.Host != "2001:db8::1" {
		t.Fatalf("markers: %+v", items)
	}
}

func TestSSHHostMarkersRetainVerificationSemantics(t *testing.T) {
	_, caKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ca, err := ssh.NewSignerFromKey(caKey)
	if err != nil {
		t.Fatal(err)
	}
	_, hostKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	hostSigner, err := ssh.NewSignerFromKey(hostKey)
	if err != nil {
		t.Fatal(err)
	}
	cert := &ssh.Certificate{Key: hostSigner.PublicKey(), CertType: ssh.HostCert, ValidPrincipals: []string{"example.com"}, ValidAfter: uint64(time.Now().Add(-time.Minute).Unix()), ValidBefore: uint64(time.Now().Add(time.Hour).Unix())}
	if err := cert.SignCert(rand.Reader, ca); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "known_hosts")
	line := "@cert-authority example.com " + string(ssh.MarshalAuthorizedKey(ca.PublicKey()))
	if err := os.WriteFile(file, []byte(line), 0600); err != nil {
		t.Fatal(err)
	}
	check, err := knownhosts.New(file)
	if err != nil {
		t.Fatal(err)
	}
	remote := &net.TCPAddr{IP: net.ParseIP("192.0.2.1"), Port: 22}
	if err := check("example.com:22", remote, cert); err != nil {
		t.Fatalf("CA certificate rejected: %v", err)
	}
	line += "@revoked example.com " + string(ssh.MarshalAuthorizedKey(ca.PublicKey()))
	if err := os.WriteFile(file, []byte(line), 0600); err != nil {
		t.Fatal(err)
	}
	check, err = knownhosts.New(file)
	if err != nil {
		t.Fatal(err)
	}
	if err := check("example.com:22", remote, cert); err == nil {
		t.Fatal("revoked CA was accepted")
	}
}

func TestSSHTrustSnapshotsAreIsolated(t *testing.T) {
	original := []byte("first")
	first := WithSSHTrust(context.Background(), original)
	second := WithSSHTrust(context.Background(), []byte("second"))
	original[0] = 'X'
	var wg sync.WaitGroup
	for _, tc := range []struct {
		ctx  context.Context
		want string
	}{{first, "first"}, {second, "second"}} {
		wg.Add(1)
		go func(ctx context.Context, want string) {
			defer wg.Done()
			got := SSHTrust(ctx)
			if string(got) != want {
				t.Errorf("got %q want %q", got, want)
			}
			got[0] = 'X'
			if string(SSHTrust(ctx)) != want {
				t.Error("context snapshot was mutated")
			}
		}(tc.ctx, tc.want)
	}
	wg.Wait()
}

func TestSSHCommandRequiresRequestTrust(t *testing.T) {
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKey(private, "test")
	if err != nil {
		t.Fatal(err)
	}
	key, err := ssh.NewPublicKey(private.Public())
	if err != nil {
		t.Fatal(err)
	}
	remote := "git@example.com:team/repo.git"
	ctx := WithSSHRemote(WithSSHKeys(context.Background(), map[string]string{remote: string(pem.EncodeToMemory(block))}), remote)
	if _, _, err := sshCommandEnvironment(ctx, []string{"fetch"}); err == nil {
		t.Fatal("missing snapshot accepted")
	}
	ctx = WithSSHTrust(ctx, []byte("example.com "+string(ssh.MarshalAuthorizedKey(key))))
	env, stop, err := sshCommandEnvironment(ctx, []string{"fetch"})
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	if env["GIT_SSH_COMMAND"] == "" {
		t.Fatal("missing isolated SSH command")
	}
}

func TestParseKnownHostsImportSizeLimit(t *testing.T) {
	if _, err := ParseKnownHostsImport(make([]byte, (1<<20)+1)); err == nil {
		t.Fatal("oversized input accepted")
	}
}

func TestSSHHostSnapshotExactAndMarkers(t *testing.T) {
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	encoded := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key)))
	remote, err := ParseSSHRemote("git@example.com:group/repo.git")
	if err != nil {
		t.Fatal(err)
	}
	input := []byte("example.com " + encoded + "\n@revoked example.com " + encoded + "\n[example.com]:2222 " + encoded + "\n")
	pinned, err := sshHostKeys([]SSHRemote{remote}, input)
	if err != nil || !strings.Contains(string(pinned), "@revoked example.com") || strings.Contains(string(pinned), ":2222") {
		t.Fatalf("pinned=%q err=%v", pinned, err)
	}
	if _, err := sshHostKeys([]SSHRemote{remote}, []byte("[example.com]:2222 "+encoded)); err == nil {
		t.Fatal("wrong port accepted")
	}
	if _, err := sshHostKeys([]SSHRemote{remote}, []byte("@revoked example.com "+encoded)); err == nil {
		t.Fatal("revocation alone must not trust endpoint")
	}
}
