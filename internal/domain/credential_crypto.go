package domain

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strings"
)

// CredentialKeyEnv is the deployment key used to encrypt subscription
// credentials at rest. The value must be 32 bytes encoded as base64.
const CredentialKeyEnv = "AGENTFLOW_CREDENTIAL_KEY"

var ErrCredentialKeyMissing = errors.New("AGENTFLOW_CREDENTIAL_KEY 未配置")

// CredentialKeyConfigured reports whether a usable credential key is
// configured. Features that persist encrypted credentials must fail closed
// when this is false.
func CredentialKeyConfigured() bool {
	_, err := credentialKey()
	return err == nil
}

func credentialKey() ([]byte, error) {
	raw := strings.TrimSpace(os.Getenv(CredentialKeyEnv))
	if raw == "" {
		return nil, ErrCredentialKeyMissing
	}
	key, err := base64.StdEncoding.DecodeString(raw)
	if err != nil || len(key) != 32 {
		return nil, fmt.Errorf("%s 必须是 32 字节 base64 密钥", CredentialKeyEnv)
	}
	return key, nil
}

// SealCredential encrypts plaintext with AES-256-GCM. The returned value is
// nonce || ciphertext.
func SealCredential(plaintext []byte) ([]byte, error) {
	key, err := credentialKey()
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

// OpenCredential decrypts a value produced by SealCredential.
func OpenCredential(sealed []byte) ([]byte, error) {
	key, err := credentialKey()
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(sealed) < gcm.NonceSize() {
		return nil, errors.New("credential ciphertext is truncated")
	}
	nonce, ciphertext := sealed[:gcm.NonceSize()], sealed[gcm.NonceSize():]
	return gcm.Open(nil, nonce, ciphertext, nil)
}
