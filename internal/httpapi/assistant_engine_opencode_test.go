package httpapi

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOpenCodeBinarySupportedRequiresV2(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
		return path
	}
	v1 := write("opencode-v1", "#!/bin/sh\necho 1.0.0\n")
	if openCodeBinarySupported(v1) {
		t.Fatal("OpenCode 1.x must not be driven by the v2 adapter")
	}
	v2 := write("opencode-v2", "#!/bin/sh\necho 2.3.0\n")
	if !openCodeBinarySupported(v2) {
		t.Fatal("OpenCode 2.x must be accepted")
	}
}
