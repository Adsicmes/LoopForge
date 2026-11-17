package domain

import (
	"encoding/base64"
	"strings"
	"testing"
)

func testCredentialKey() string {
	return base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
}

func TestSealOpenCredential(t *testing.T) {
	t.Setenv(CredentialKeyEnv, testCredentialKey())
	plain := []byte(`{"tokens":{"access_token":"a","refresh_token":"r"}}`)
	sealed, err := SealCredential(plain)
	if err != nil {
		t.Fatal(err)
	}
	if string(sealed) == string(plain) {
		t.Fatal("credential was not encrypted")
	}
	opened, err := OpenCredential(sealed)
	if err != nil {
		t.Fatal(err)
	}
	if string(opened) != string(plain) {
		t.Fatalf("opened = %q", opened)
	}
	sealed[len(sealed)-1] ^= 0xff
	if _, err := OpenCredential(sealed); err == nil {
		t.Fatal("tampered ciphertext was accepted")
	}
}

func TestCredentialKeyMissingFailsClosed(t *testing.T) {
	t.Setenv(CredentialKeyEnv, "")
	if CredentialKeyConfigured() {
		t.Fatal("empty key should not be configured")
	}
	if _, err := SealCredential([]byte("secret")); err == nil || !strings.Contains(err.Error(), CredentialKeyEnv) {
		t.Fatalf("SealCredential error = %v", err)
	}
}

func TestCredentialKeyMustBe32Bytes(t *testing.T) {
	t.Setenv(CredentialKeyEnv, base64.StdEncoding.EncodeToString([]byte("short")))
	if CredentialKeyConfigured() {
		t.Fatal("short key should not be configured")
	}
}
