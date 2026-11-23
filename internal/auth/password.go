package auth

import (
	"bufio"
	"bytes"
	"crypto/subtle"
	_ "embed"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

// Argon2id parameters (spec 10): 19 MiB, 2 iterations, parallelism 1, 16-byte salt,
// 32-byte key. Stored with every hash and upgraded on login.
const (
	argonMemory  = 19 * 1024 // KiB
	argonTime    = 2
	argonThreads = 1
	saltLen      = 16
	keyLen       = 32
	argonVersion = argon2.Version
)

// Password policy (spec 10).
const (
	MinPasswordLen = 12
	maxPasswordLen = 1024 // bounds hashing work per request
)

// Password policy reasons of a PasswordError.
const (
	ReasonTooShort  = "too_short"
	ReasonTooLong   = "too_long"
	ReasonTooCommon = "too_common"
)

// PasswordError reports a password that does not meet the policy.
type PasswordError struct{ Reason string }

func (e *PasswordError) Error() string { return "auth: weak password: " + e.Reason }

// errBadHash marks a stored hash that cannot be parsed.
var errBadHash = errors.New("auth: malformed password hash")

//go:embed common-passwords.txt
var commonList []byte

// commonPasswords loads the embedded list of common passwords (lower case).
func commonPasswords() map[string]bool {
	out := map[string]bool{}
	sc := bufio.NewScanner(bytes.NewReader(commonList))
	for sc.Scan() {
		if line := strings.TrimSpace(sc.Text()); line != "" && line[0] != '#' {
			out[strings.ToLower(line)] = true
		}
	}
	return out
}

// CheckPassword applies the password policy without hashing, so CLI jobs can reject a
// weak password before they make a backup.
func CheckPassword(pw string) error { return checkPassword(pw, commonPasswords()) }

// checkPassword applies the policy: length 12..1024 characters, not a common password and
// not one repeated character. There are no composition rules (spec 10).
func checkPassword(pw string, common map[string]bool) error {
	n := utf8.RuneCountInString(pw)
	switch {
	case n < MinPasswordLen:
		return &PasswordError{Reason: ReasonTooShort}
	case len(pw) > maxPasswordLen:
		return &PasswordError{Reason: ReasonTooLong}
	case common[strings.ToLower(pw)] || strings.Count(pw, pw[:1]) == len(pw):
		return &PasswordError{Reason: ReasonTooCommon}
	}
	return nil
}

// argonParams are the parameters of one stored hash.
type argonParams struct {
	memory  uint32
	time    uint32
	threads uint8
}

func currentParams() argonParams {
	return argonParams{memory: argonMemory, time: argonTime, threads: argonThreads}
}

// hashPassword returns the PHC string of pw with a fresh salt from rnd.
func hashPassword(pw string, rnd io.Reader) (string, error) {
	salt := make([]byte, saltLen)
	if _, err := io.ReadFull(rnd, salt); err != nil {
		return "", fmt.Errorf("auth: salt: %w", err)
	}
	p := currentParams()
	key := argon2.IDKey([]byte(pw), salt, p.time, p.memory, p.threads, keyLen)
	enc := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$%s$m=%d,t=%d,p=%d$%s$%s", versionField(), p.memory, p.time,
		p.threads, enc.EncodeToString(salt), enc.EncodeToString(key)), nil
}

// verifyPassword reports whether pw matches the PHC hash and whether the hash uses old
// parameters and should be replaced.
func verifyPassword(pw, hash string) (ok, upgrade bool, err error) {
	p, salt, key, err := parseHash(hash)
	if err != nil {
		return false, false, err
	}
	got := argon2.IDKey([]byte(pw), salt, p.time, p.memory, p.threads, uint32(len(key)))
	ok = subtle.ConstantTimeCompare(got, key) == 1
	return ok, ok && (p != currentParams() || len(salt) != saltLen || len(key) != keyLen), nil
}

func parseHash(hash string) (argonParams, []byte, []byte, error) {
	parts := strings.Split(hash, "$")
	const fields = 6 // "", "argon2id", "v=19", params, salt, key
	if len(parts) != fields || parts[1]+"$"+parts[2] != "argon2id$"+versionField() {
		return argonParams{}, nil, nil, errBadHash
	}
	p, err := parseParams(parts[3])
	if err != nil {
		return argonParams{}, nil, nil, err
	}
	salt, errS := base64.RawStdEncoding.DecodeString(parts[4])
	key, errK := base64.RawStdEncoding.DecodeString(parts[5])
	if errS != nil || errK != nil || len(key) == 0 {
		return argonParams{}, nil, nil, errBadHash
	}
	return p, salt, key, nil
}

func versionField() string { return fmt.Sprintf("v=%d", argonVersion) }

func parseParams(s string) (argonParams, error) {
	var p argonParams
	_, err := fmt.Sscanf(s, "m=%d,t=%d,p=%d", &p.memory, &p.time, &p.threads)
	if err != nil || p.memory == 0 || p.time == 0 || p.threads == 0 {
		return argonParams{}, errBadHash
	}
	return p, nil
}
