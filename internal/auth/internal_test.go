package auth

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/argon2"
)

var errRand = errors.New("rand failed")

type failReader struct{}

func (failReader) Read([]byte) (int, error) { return 0, errRand }

func TestCheckPassword(t *testing.T) {
	t.Parallel()
	common := map[string]bool{"passwordpassword": true}
	cases := []struct {
		name, pw, reason string
	}{
		{"short", "elevenchars", ReasonTooShort},
		{"short runes", strings.Repeat("ä", 11), ReasonTooShort},
		{"too long", strings.Repeat("ab", maxPasswordLen), ReasonTooLong},
		{"common mixed case", "PasswordPassword", ReasonTooCommon},
		{"repeated", strings.Repeat("x", 20), ReasonTooCommon},
		{"ok", "correct horse battery", ""},
		{"ok runes", strings.Repeat("ä", 12) + "b", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := checkPassword(tc.pw, common)
			if tc.reason == "" {
				require.NoError(t, err)
				return
			}
			var pe *PasswordError
			require.ErrorAs(t, err, &pe)
			assert.Equal(t, tc.reason, pe.Reason)
			assert.Contains(t, err.Error(), tc.reason)
		})
	}
}

func TestCommonPasswordsEmbedded(t *testing.T) {
	t.Parallel()
	common := commonPasswords()
	require.NotEmpty(t, common)
	for k := range common {
		assert.Equal(t, strings.ToLower(k), k)
		assert.NotContains(t, k, "#")
		break
	}
}

func TestHashVerify(t *testing.T) {
	t.Parallel()
	h, err := hashPassword("secret password", rand.Reader)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(h, "$argon2id$v=19$m=19456,t=2,p=1$"))
	ok, upgrade, err := verifyPassword("secret password", h)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.False(t, upgrade)
	ok, upgrade, err = verifyPassword("other", h)
	require.NoError(t, err)
	assert.False(t, ok)
	assert.False(t, upgrade, "no upgrade for a wrong password")

	_, err = hashPassword("x", failReader{})
	require.ErrorIs(t, err, errRand)
}

func TestVerifyUpgrade(t *testing.T) {
	t.Parallel()
	salt := bytes.Repeat([]byte{7}, saltLen)
	enc := base64.RawStdEncoding
	mk := func(salt []byte, keyLen uint32) string {
		key := argon2.IDKey([]byte("pw"), salt, 1, 1024, 1, keyLen)
		return fmt.Sprintf("$argon2id$v=19$m=1024,t=1,p=1$%s$%s", enc.EncodeToString(salt),
			enc.EncodeToString(key))
	}
	for name, h := range map[string]string{
		"low params": mk(salt, keyLen),
		"short salt": mk(salt[:8], keyLen),
		"short key":  mk(salt, 16),
	} {
		ok, upgrade, err := verifyPassword("pw", h)
		require.NoError(t, err, name)
		assert.True(t, ok, name)
		assert.True(t, upgrade, name)
	}
}

func TestParseHashMalformed(t *testing.T) {
	t.Parallel()
	for name, h := range map[string]string{
		"empty":        "",
		"plain":        "hunter2",
		"wrong algo":   "$argon2i$v=19$m=1,t=1,p=1$AAAA$AAAA",
		"wrong ver":    "$argon2id$v=18$m=1,t=1,p=1$AAAA$AAAA",
		"too few":      "$argon2id$v=19$m=1,t=1,p=1$AAAA",
		"bad params":   "$argon2id$v=19$m=x,t=1,p=1$AAAA$AAAA",
		"zero memory":  "$argon2id$v=19$m=0,t=1,p=1$AAAA$AAAA",
		"bad salt":     "$argon2id$v=19$m=8,t=1,p=1$!!!$AAAA",
		"bad key":      "$argon2id$v=19$m=8,t=1,p=1$AAAA$!!!",
		"empty key":    "$argon2id$v=19$m=8,t=1,p=1$AAAA$",
		"zero threads": "$argon2id$v=19$m=8,t=1,p=0$AAAA$AAAA",
	} {
		_, _, err := verifyPassword("pw", h)
		require.ErrorIs(t, err, errBadHash, name)
	}
}

func TestSplitAPIToken(t *testing.T) {
	t.Parallel()
	id, secret := "abcdefgh", strings.Repeat("S", tokenSecretLen)
	gotID, gotSecret, ok := splitAPIToken("pvm_" + id + "_" + secret)
	require.True(t, ok)
	assert.Equal(t, id, gotID)
	assert.Equal(t, secret, gotSecret)
	for name, tok := range map[string]string{
		"empty":        "",
		"no prefix":    id + "_" + secret,
		"no separator": "pvm_" + id + secret,
		"short id":     "pvm_abc_" + secret,
		"long id":      "pvm_abcdefghi_" + secret,
		"short secret": "pvm_" + id + "_short",
		"long secret":  "pvm_" + id + "_" + secret + "x",
	} {
		_, _, ok := splitAPIToken(tok)
		assert.False(t, ok, name)
	}
}

func TestNewSetupCode(t *testing.T) {
	t.Parallel()
	code, err := newSetupCode(rand.Reader)
	require.NoError(t, err)
	assert.Len(t, code, setupCodeLen)
	for _, r := range code {
		assert.Contains(t, crockford, string(r))
	}
	zero, err := newSetupCode(bytes.NewReader(make([]byte, 8)))
	require.NoError(t, err)
	assert.Equal(t, "00000000", zero)
	full, err := newSetupCode(bytes.NewReader(bytes.Repeat([]byte{0xff}, 8)))
	require.NoError(t, err)
	assert.Equal(t, "ZZZZZZZZ", full)
	_, err = newSetupCode(failReader{})
	require.ErrorIs(t, err, errRand)
}

func TestNormaliseAndEqualCode(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "0111234", normaliseCode(" o-I l 1-234 "))
	assert.Equal(t, "ABCD1234", normaliseCode("abcd-1234"))
	assert.True(t, equalCode("ABCD1234", "abcd-1234"))
	assert.False(t, equalCode("ABCD1234", "abcd-1235"))
	assert.False(t, equalCode("", ""), "an empty code never matches")
}

func TestRandomBase62(t *testing.T) {
	t.Parallel()
	s, err := randomBase62(rand.Reader, 40)
	require.NoError(t, err)
	assert.Len(t, s, 40)
	for _, r := range s {
		assert.Contains(t, base62, string(r))
	}
	// the first batch is rejected (>= 248), the second yields 0,1,2
	in := append(bytes.Repeat([]byte{0xff}, 3), 0, 1, 2)
	s, err = randomBase62(bytes.NewReader(in), 3)
	require.NoError(t, err)
	assert.Equal(t, "012", s)
	_, err = randomBase62(failReader{}, 3)
	require.ErrorIs(t, err, errRand)
}

func TestTokenHelpers(t *testing.T) {
	t.Parallel()
	a, err := newSessionToken(rand.Reader)
	require.NoError(t, err)
	b, err := newSessionToken(rand.Reader)
	require.NoError(t, err)
	assert.NotEqual(t, a, b)
	assert.Len(t, a, 43)
	assert.Len(t, hashToken(a), 32)
	assert.Equal(t, hashToken(a), hashToken(a))
	_, err = newSessionToken(failReader{})
	require.ErrorIs(t, err, errRand)

	id, secret, err := newAPIToken(rand.Reader)
	require.NoError(t, err)
	assert.Len(t, id, tokenIDLen)
	assert.Len(t, secret, tokenSecretLen)
	_, _, err = newAPIToken(failReader{})
	require.ErrorIs(t, err, errRand)
	_, _, err = newAPIToken(&limitedReader{n: tokenIDLen})
	require.ErrorIs(t, err, errRand)
}

// limitedReader yields n zero bytes and then fails.
type limitedReader struct{ n int }

func (r *limitedReader) Read(p []byte) (int, error) {
	if r.n <= 0 {
		return 0, errRand
	}
	n := min(len(p), r.n)
	r.n -= n
	return n, nil
}

func TestDelay(t *testing.T) {
	t.Parallel()
	assert.Equal(t, time.Second, delay(0))
	assert.Equal(t, 2*time.Second, delay(1))
	assert.Equal(t, 512*time.Second, delay(9))
	assert.Equal(t, maxDelay, delay(10))
	assert.Equal(t, maxDelay, delay(11))
	assert.Equal(t, maxDelay, delay(100))
}

func TestLimiter(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	l := newLimiter()
	for range freeFailures - 1 {
		l.fail(now, "ip:a", "user:x")
	}
	assert.Zero(t, l.wait(now, "ip:a", "user:x"), "the first failures are free")
	l.fail(now, "ip:a", "user:x")
	assert.Equal(t, time.Second, l.wait(now, "ip:a"))
	assert.Equal(t, 400*time.Millisecond, l.wait(now.Add(600*time.Millisecond), "user:x"))
	assert.Zero(t, l.wait(now.Add(time.Second), "ip:a"))
	assert.Zero(t, l.wait(now, "ip:other"))

	l.fail(now, "ip:a")
	assert.Equal(t, 2*time.Second, l.wait(now, "ip:a"))
	l.fail(now, "ip:a")
	assert.Equal(t, 4*time.Second, l.wait(now, "ip:a"))
	assert.Equal(t, 4*time.Second, l.wait(now, "ip:other", "ip:a", "user:x"), "maximum of keys")
	for range 30 {
		l.fail(now, "ip:a")
	}
	assert.Equal(t, maxDelay, l.wait(now, "ip:a"))

	l.succeed("ip:a", "user:x")
	assert.Zero(t, l.wait(now, "ip:a", "user:x"))
	assert.Empty(t, l.keys)
}

func TestLimiterPrune(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	l := newLimiter()
	l.fail(now, "old")
	for range freeFailures {
		l.fail(now, "blocked")
	}
	l.fail(now.Add(maxDelay-time.Second), "recent")
	assert.Len(t, l.keys, 3)
	// "old" is older than 15 min, "blocked" is blocked for 1 s only and also stale
	l.fail(now.Add(maxDelay+time.Minute), "new")
	assert.NotContains(t, l.keys, "old")
	assert.NotContains(t, l.keys, "blocked")
	assert.Contains(t, l.keys, "recent")
	assert.Contains(t, l.keys, "new")
}

func TestMapStoreAndNames(t *testing.T) {
	t.Parallel()
	_, err := checkName("  ")
	require.ErrorIs(t, err, ErrInvalid)
	_, err = checkName("a\nb")
	require.ErrorIs(t, err, ErrInvalid)
	_, err = checkName(strings.Repeat("a", maxNameLen+1))
	require.ErrorIs(t, err, ErrInvalid)
	n, err := checkName("  Alice ")
	require.NoError(t, err)
	assert.Equal(t, "Alice", n)
	require.NoError(t, checkLang(""))
	require.NoError(t, checkLang("de"))
	require.ErrorIs(t, checkLang("fr"), ErrInvalid)
	other := errors.New("boom")
	assert.Equal(t, other, mapStore(other))
	assert.NoError(t, mapStore(nil))
}

func TestDetailJSON(t *testing.T) {
	t.Parallel()
	assert.Nil(t, detailJSON(""))
	assert.JSONEq(t, `{"a":1}`, string(detailJSON(`{"a":1}`)))
	assert.JSONEq(t, `"plain text"`, string(detailJSON("plain text")))
}
