package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"strings"
)

const encryptedValuePrefix = "v1:"

type Box struct {
	key []byte
}

func NewBox(rawKey string) (*Box, error) {
	trimmed := strings.TrimSpace(rawKey)
	if trimmed == "" {
		return nil, nil
	}
	decoded, err := base64.StdEncoding.DecodeString(trimmed)
	if err != nil {
		return nil, fmt.Errorf("decode APP_SETTINGS_MASTER_KEY: %w", err)
	}
	switch len(decoded) {
	case 16, 24, 32:
		return &Box{key: decoded}, nil
	default:
		return nil, fmt.Errorf("APP_SETTINGS_MASTER_KEY must decode to 16, 24, or 32 bytes")
	}
}

func (b *Box) Ready() bool {
	return b != nil && len(b.key) > 0
}

func (b *Box) Encrypt(plaintext string) (string, error) {
	if !b.Ready() {
		return "", fmt.Errorf("app settings secret storage is not configured")
	}
	block, err := aes.NewCipher(b.key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nil, nonce, []byte(plaintext), nil)
	payload := append(nonce, sealed...)
	return encryptedValuePrefix + base64.StdEncoding.EncodeToString(payload), nil
}

func (b *Box) Decrypt(ciphertext string) (string, error) {
	if strings.TrimSpace(ciphertext) == "" {
		return "", nil
	}
	if !b.Ready() {
		return "", fmt.Errorf("app settings secret storage is not configured")
	}
	if !strings.HasPrefix(ciphertext, encryptedValuePrefix) {
		return "", fmt.Errorf("unsupported encrypted settings payload")
	}
	encoded := strings.TrimPrefix(ciphertext, encryptedValuePrefix)
	payload, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("decode encrypted settings payload: %w", err)
	}
	block, err := aes.NewCipher(b.key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(payload) < gcm.NonceSize() {
		return "", fmt.Errorf("encrypted settings payload is truncated")
	}
	nonce := payload[:gcm.NonceSize()]
	sealed := payload[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, sealed, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt encrypted settings payload: %w", err)
	}
	return string(plaintext), nil
}
