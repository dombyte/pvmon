package auth

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"io"
	"strings"
)

// Token sizes (spec 10).
const (
	sessionBytes   = 32 // 256-bit session token
	tokenIDLen     = 8
	tokenSecretLen = 32
	setupCodeLen   = 8
	tokenPrefix    = "pvm_"
	base62         = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	crockford      = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
	// base62Limit is the largest multiple of 62 that fits a byte; bytes at or above it are
	// rejected so every character is equally likely.
	base62Limit = 248
	b32Bits     = 5
	b32Mask     = 0x1f
	byteBits    = 8
)

// newSessionToken returns a random session token (base64url, no padding).
func newSessionToken(rnd io.Reader) (string, error) {
	b := make([]byte, sessionBytes)
	if _, err := io.ReadFull(rnd, b); err != nil {
		return "", fmt.Errorf("auth: session token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// hashToken returns the SHA-256 under which a session token or token secret is stored.
func hashToken(s string) []byte {
	h := sha256.Sum256([]byte(s))
	return h[:]
}

// randomBase62 returns n uniformly random base62 characters.
func randomBase62(rnd io.Reader, n int) (string, error) {
	var sb strings.Builder
	buf := make([]byte, n)
	for sb.Len() < n {
		if _, err := io.ReadFull(rnd, buf); err != nil {
			return "", fmt.Errorf("auth: random: %w", err)
		}
		for _, b := range buf {
			if b < base62Limit && sb.Len() < n {
				sb.WriteByte(base62[int(b)%len(base62)])
			}
		}
	}
	return sb.String(), nil
}

// newAPIToken returns the ID and secret of a token pvm_<id>_<secret>.
func newAPIToken(rnd io.Reader) (id, secret string, err error) {
	if id, err = randomBase62(rnd, tokenIDLen); err != nil {
		return "", "", err
	}
	if secret, err = randomBase62(rnd, tokenSecretLen); err != nil {
		return "", "", err
	}
	return id, secret, nil
}

// splitAPIToken parses pvm_<id>_<secret>; ok is false for any other shape.
func splitAPIToken(t string) (id, secret string, ok bool) {
	rest, found := strings.CutPrefix(t, tokenPrefix)
	if !found {
		return "", "", false
	}
	id, secret, found = strings.Cut(rest, "_")
	if !found || len(id) != tokenIDLen || len(secret) != tokenSecretLen {
		return "", "", false
	}
	return id, secret, true
}

// newSetupCode returns 8 Crockford base32 characters (40 random bits).
func newSetupCode(rnd io.Reader) (string, error) {
	b := make([]byte, setupCodeLen*b32Bits/byteBits)
	if _, err := io.ReadFull(rnd, b); err != nil {
		return "", fmt.Errorf("auth: setup code: %w", err)
	}
	var v uint64
	for _, x := range b {
		v = v<<byteBits | uint64(x)
	}
	out := make([]byte, setupCodeLen)
	for i := setupCodeLen - 1; i >= 0; i-- {
		out[i] = crockford[v&b32Mask]
		v >>= b32Bits
	}
	return string(out), nil
}

// normaliseCode maps user input onto Crockford base32: upper case, O→0, I/L→1, no
// separators or spaces.
func normaliseCode(s string) string {
	r := strings.NewReplacer("-", "", " ", "", "O", "0", "I", "1", "L", "1")
	return r.Replace(strings.ToUpper(strings.TrimSpace(s)))
}

// equalCode compares a setup code in constant time.
func equalCode(want, got string) bool {
	return want != "" && subtle.ConstantTimeCompare([]byte(want), []byte(normaliseCode(got))) == 1
}
