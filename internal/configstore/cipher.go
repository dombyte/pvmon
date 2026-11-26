package configstore

import (
	"crypto/cipher"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"

	"golang.org/x/crypto/chacha20poly1305"
)

// encPrefix marks an encrypted secret in a stored body (spec 10).
const encPrefix = "enc:v1:"

// keyMode is the file mode of secret.key.
const keyMode = 0o600

var (
	// ErrNoKey is returned when a secret is read without a usable secret.key.
	ErrNoKey = errors.New("configstore: secret.key is missing")
	// ErrBadSecret is returned for a stored secret that cannot be decrypted.
	ErrBadSecret = errors.New("configstore: secret cannot be decrypted")
)

// Cipher encrypts the secrets of the UI configuration with XChaCha20-Poly1305 and the
// 256-bit key in secret.key. A Cipher without key (CLI jobs that must not create one)
// fails every Seal and Open with ErrNoKey.
type Cipher struct {
	aead cipher.AEAD
	rand io.Reader
}

// LoadKey reads the key file; a missing file is created with a new key when create is
// set (server start), else the returned Cipher has no key.
func LoadKey(path string, create bool, rnd io.Reader) (*Cipher, error) {
	key, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		if !create {
			return &Cipher{rand: rnd}, nil
		}
		key, err = newKey(path, rnd)
	}
	if err != nil {
		return nil, fmt.Errorf("configstore: secret key: %w", err)
	}
	if key, err = decodeKey(key); err != nil {
		return nil, fmt.Errorf("configstore: secret key: %w", err)
	}
	return NewCipher(key, rnd)
}

// decodeKey accepts the base64 text form and the raw 32 bytes that pvmon up to v0.2.0
// wrote.
func decodeKey(data []byte) ([]byte, error) {
	if len(data) == chacha20poly1305.KeySize {
		return data, nil
	}
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(data)))
	if err != nil {
		return nil, fmt.Errorf("decode base64: %w", err)
	}
	return key, nil
}

func newKey(path string, rnd io.Reader) ([]byte, error) {
	key := make([]byte, chacha20poly1305.KeySize)
	if _, err := io.ReadFull(rnd, key); err != nil {
		return nil, fmt.Errorf("generate: %w", err)
	}
	text := base64.StdEncoding.EncodeToString(key) + "\n"
	if err := os.WriteFile(path, []byte(text), keyMode); err != nil {
		return nil, fmt.Errorf("write: %w", err)
	}
	return []byte(text), nil
}

// NewCipher builds a Cipher from a 32-byte key.
func NewCipher(key []byte, rnd io.Reader) (*Cipher, error) {
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, fmt.Errorf("configstore: secret key: %w", err)
	}
	return &Cipher{aead: aead, rand: rnd}, nil
}

// Seal encrypts plain into enc:v1:<base64(nonce+ciphertext)>.
func (c *Cipher) Seal(plain string) (string, error) {
	if c.aead == nil {
		return "", ErrNoKey
	}
	nonce := make([]byte, c.aead.NonceSize(), c.aead.NonceSize()+len(plain)+c.aead.Overhead())
	if _, err := io.ReadFull(c.rand, nonce); err != nil {
		return "", fmt.Errorf("configstore: nonce: %w", err)
	}
	out := c.aead.Seal(nonce, nonce, []byte(plain), nil)
	return encPrefix + base64.StdEncoding.EncodeToString(out), nil
}

// Open decrypts a value written by Seal.
func (c *Cipher) Open(sealed string) (string, error) {
	if c.aead == nil {
		return "", ErrNoKey
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(sealed, encPrefix))
	if err != nil || !strings.HasPrefix(sealed, encPrefix) || len(raw) < c.aead.NonceSize() {
		return "", ErrBadSecret
	}
	n := c.aead.NonceSize()
	plain, err := c.aead.Open(nil, raw[:n], raw[n:], nil)
	if err != nil {
		return "", ErrBadSecret
	}
	return string(plain), nil
}
