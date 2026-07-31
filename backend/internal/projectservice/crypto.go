package projectservice

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"time"
)

type realClock struct{}

func (realClock) Now() time.Time { return time.Now().UTC() }

func generateCredential(reader io.Reader, prefix string) (string, error) {
	value := make([]byte, 32)
	if _, err := io.ReadFull(reader, value); err != nil {
		return "", fmt.Errorf("%w: %v", ErrCredentialGeneration, err)
	}
	return prefix + base64.RawURLEncoding.EncodeToString(value), nil
}

func apiKeyDigest(apiKey string) [sha256.Size]byte {
	return sha256.Sum256([]byte(apiKey))
}

func encryptSecret(reader io.Reader, key []byte, projectID string, secretVersion, keyVersion int, plaintext string) ([]byte, []byte, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return nil, nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(reader, nonce); err != nil {
		return nil, nil, fmt.Errorf("%w: %v", ErrCredentialGeneration, err)
	}
	ciphertext := gcm.Seal(nil, nonce, []byte(plaintext), secretAAD(projectID, secretVersion, keyVersion))
	return ciphertext, nonce, nil
}

func decryptSecret(key, ciphertext, nonce []byte, projectID string, secretVersion, keyVersion int) (string, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return "", err
	}
	plaintext, err := gcm.Open(nil, nonce, ciphertext, secretAAD(projectID, secretVersion, keyVersion))
	if err != nil {
		return "", fmt.Errorf("%w", ErrWebhookSecretDecryption)
	}
	return string(plaintext), nil
}

func newGCM(key []byte) (cipher.AEAD, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("%w: AES-256 key must contain 32 bytes", ErrEncryptionConfiguration)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrEncryptionConfiguration, err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrEncryptionConfiguration, err)
	}
	return gcm, nil
}

func secretAAD(projectID string, secretVersion, keyVersion int) []byte {
	return []byte(fmt.Sprintf("device-platform:webhook-secret:v1:%s:%d:%d", projectID, secretVersion, keyVersion))
}

func randomUUID(reader io.Reader) (string, error) {
	value := make([]byte, 16)
	if _, err := io.ReadFull(reader, value); err != nil {
		return "", fmt.Errorf("%w: %v", ErrCredentialGeneration, err)
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", value[0:4], value[4:6], value[6:8], value[8:10], value[10:16]), nil
}
