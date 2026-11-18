package gitwork

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"net"
	"net/netip"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

func TestScanAddressPolicy(t *testing.T) {
	for _, tc := range []struct {
		address string
		allowed bool
	}{
		{"10.0.0.1", true}, {"192.168.1.1", true}, {"8.8.8.8", true},
		{"127.0.0.1", false}, {"169.254.169.254", false}, {"224.0.0.1", false},
		{"::1", false}, {"fe80::1", false}, {"fd00::1", true},
		{"::ffff:127.0.0.1", false}, {"2001:db8::1", false},
	} {
		if got := allowedScanIP(netip.MustParseAddr(tc.address)); got != tc.allowed {
			t.Errorf("%s: got %v", tc.address, got)
		}
	}
}

func TestScanReturnsUntrustedCandidates(t *testing.T) {
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(private)
	if err != nil {
		t.Fatal(err)
	}
	server := &ssh.ServerConfig{NoClientAuth: true}
	server.AddHostKey(signer)
	resolve := func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("10.0.0.1")}, nil
	}
	dial := func(context.Context, string) (net.Conn, error) {
		client, peer := net.Pipe()
		buffered := &scanTestConn{Conn: peer, writes: make(chan []byte, 16)}
		go func() {
			for data := range buffered.writes {
				if _, err := peer.Write(data); err != nil {
					return
				}
			}
		}()
		go func() {
			defer peer.Close()
			defer close(buffered.writes)
			conn, _, _, err := ssh.NewServerConn(buffered, server)
			if err == nil {
				_ = conn.Close()
			}
		}()
		return client, nil
	}
	items, err := scanSSHHost(context.Background(), "git.example", 2222, resolve, dial)
	if err != nil || len(items) != 1 || items[0].Host != "git.example" || items[0].Port != 2222 || items[0].Fingerprint != ssh.FingerprintSHA256(signer.PublicKey()) {
		t.Fatalf("candidates=%+v err=%v", items, err)
	}
}

type scanTestConn struct {
	net.Conn
	writes chan []byte
}

func (c *scanTestConn) Write(data []byte) (int, error) {
	c.writes <- append([]byte(nil), data...)
	return len(data), nil
}

func TestScanValidatesDNSAndDialsOnlyResolvedIP(t *testing.T) {
	called := false
	resolve := func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("8.8.8.8"), netip.MustParseAddr("169.254.169.254")}, nil
	}
	dial := func(context.Context, string) (net.Conn, error) {
		called = true
		return nil, errors.New("not connected")
	}
	if _, err := scanSSHHost(context.Background(), "example.com", 22, resolve, dial); err == nil || called {
		t.Fatal("special DNS result must prevent every connection")
	}
	resolve = func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("8.8.8.8")}, nil
	}
	dial = func(_ context.Context, address string) (net.Conn, error) {
		called = true
		if !strings.HasPrefix(address, "8.8.8.8:") {
			t.Errorf("dialed unchecked address %q", address)
		}
		return nil, errors.New("not connected")
	}
	_, _ = scanSSHHost(context.Background(), "example.com", 22, resolve, dial)
	if !called {
		t.Fatal("no dial attempted")
	}
}
