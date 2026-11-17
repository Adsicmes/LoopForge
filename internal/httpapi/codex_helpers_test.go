package httpapi

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSanitizeLoginError(t *testing.T) {
	raw := "\x1b[31mWarning: ignore this\x1b[0m\n" +
		"  first line  \r\n" +
		"Authorization: Bearer abcdefghijklmnop\n" +
		"API_KEY=supersecret\n" +
		"second\nthird\nfourth"
	got := sanitizeLoginError(raw)
	if strings.Contains(got, "Warning:") || strings.Contains(got, "abcdefghijklmnop") || strings.Contains(got, "supersecret") {
		t.Fatalf("sensitive text survived: %q", got)
	}
	if !strings.Contains(got, "first line") || strings.Contains(got, "\x1b") {
		t.Fatalf("cleaned text = %q", got)
	}
	if len(strings.Split(got, "；")) > 3 {
		t.Fatalf("kept too many lines: %q", got)
	}
	if long := sanitizeLoginError(strings.Repeat("错误", 400)); len(long) > 500 {
		t.Fatalf("long message = %d bytes", len(long))
	}
	if sanitizeLoginError(" \n Warning: only \n ") != "" {
		t.Fatal("empty login error was not empty")
	}
}

func TestLimitedBuffer(t *testing.T) {
	write := func(b *limitedBuffer, text string) int {
		n, err := b.Write([]byte(text))
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	b := &limitedBuffer{limit: 4}
	if n := write(b, "abcdef"); n != 6 || b.String() != "abcd" {
		t.Fatalf("write = %d %q", n, b.String())
	}
	if n := write(b, "x"); n != 1 || b.String() != "abcd" {
		t.Fatalf("overflow write = %d %q", n, b.String())
	}
	exact := &limitedBuffer{limit: 4}
	write(exact, "ab")
	write(exact, "cd")
	if exact.String() != "abcd" {
		t.Fatalf("exact fill = %q", exact.String())
	}
}

func TestCodexLoginEnvironment(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PATH", "/usr/bin")
	t.Setenv("HTTP_PROXY", "http://proxy.example")
	t.Setenv("NO_PROXY", "   ")
	env := codexLoginEnvironment(home, filepath.Join(home, ".codex"))
	joined := strings.Join(env, "\n")
	for _, want := range []string{"HOME=" + home, "TMPDIR=" + filepath.Join(home, "tmp"), "CODEX_HOME=" + filepath.Join(home, ".codex"), "PATH=/usr/bin", "HTTP_PROXY=http://proxy.example"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("environment missing %q: %v", want, env)
		}
	}
	if strings.Contains(joined, "NO_PROXY=") {
		t.Fatalf("blank proxy was passed: %v", env)
	}
	if _, err := os.Stat(filepath.Join(home, "tmp")); err != nil {
		t.Fatalf("tmp dir missing: %v", err)
	}
}

func TestCopyGatewayHeaders(t *testing.T) {
	dst := http.Header{}
	src := http.Header{
		"Authorization": {"Bearer secret"},
		"Host":          {"upstream.example"},
		"X-Test":        {"one", "two"},
		"Connection":    {"keep-alive"},
	}
	copyGatewayHeaders(dst, src)
	if dst.Get("Authorization") != "" || dst.Get("Host") != "" || dst.Get("Connection") != "" {
		t.Fatalf("hop headers copied: %v", dst)
	}
	if values := dst.Values("X-Test"); len(values) != 2 || values[0] != "one" || values[1] != "two" {
		t.Fatalf("X-Test = %v", values)
	}
}
