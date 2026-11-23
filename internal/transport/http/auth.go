package http

import (
	"crypto/md5" //nolint:gosec // required by HTTP digest auth (RFC 7616), not for secrecy
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"hash"
	"strings"
)

func basicAuth(user, password string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+password))
}

// challenge is a parsed WWW-Authenticate: Digest header plus the nonce counter.
type challenge struct {
	realm, nonce, opaque, qop, algorithm string
	nc                                   int
}

func parseChallenge(header string) *challenge {
	rest, ok := strings.CutPrefix(header, "Digest ")
	if !ok {
		return nil
	}
	c := &challenge{algorithm: "MD5"}
	var qop string
	fields := map[string]*string{
		"realm": &c.realm, "nonce": &c.nonce, "opaque": &c.opaque, "qop": &qop,
		"algorithm": &c.algorithm,
	}
	for _, part := range splitParams(rest) {
		k, v, _ := strings.Cut(part, "=")
		if f, ok := fields[strings.ToLower(strings.TrimSpace(k))]; ok {
			*f = strings.Trim(strings.TrimSpace(v), `"`)
		}
	}
	c.qop = firstQop(qop)
	return c
}

// splitParams splits on commas outside quotes.
func splitParams(s string) []string {
	var out []string
	inQuote, start := false, 0
	for i, r := range s {
		switch {
		case r == '"':
			inQuote = !inQuote
		case r == ',' && !inQuote:
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return append(out, s[start:])
}

func firstQop(v string) string {
	for _, q := range strings.Split(v, ",") {
		if strings.TrimSpace(q) == "auth" {
			return "auth"
		}
	}
	return ""
}

func (c *challenge) hasher() func() hash.Hash {
	if strings.HasPrefix(strings.ToUpper(c.algorithm), "SHA-256") {
		return sha256.New
	}
	return md5.New
}

func (c *challenge) h(s string) string {
	hh := c.hasher()()
	hh.Write([]byte(s))
	return hex.EncodeToString(hh.Sum(nil))
}

// authorize returns the Authorization header for one request (RFC 7616, qop=auth or none).
func (c *challenge) authorize(user, password, method, uri string) string {
	c.nc++
	cnonce := newCnonce()
	ha1 := c.h(user + ":" + c.realm + ":" + password)
	if strings.HasSuffix(strings.ToLower(c.algorithm), "-sess") {
		ha1 = c.h(ha1 + ":" + c.nonce + ":" + cnonce)
	}
	ha2 := c.h(method + ":" + uri)
	nc := fmt.Sprintf("%08x", c.nc)
	var response string
	if c.qop == "auth" {
		response = c.h(strings.Join([]string{ha1, c.nonce, nc, cnonce, c.qop, ha2}, ":"))
	} else {
		response = c.h(ha1 + ":" + c.nonce + ":" + ha2)
	}
	parts := []string{
		fmt.Sprintf(`username="%s"`, user), fmt.Sprintf(`realm="%s"`, c.realm),
		fmt.Sprintf(`nonce="%s"`, c.nonce), fmt.Sprintf(`uri="%s"`, uri),
		"algorithm=" + c.algorithm, fmt.Sprintf(`response="%s"`, response),
	}
	if c.qop == "auth" {
		parts = append(parts, "qop=auth", "nc="+nc, fmt.Sprintf(`cnonce="%s"`, cnonce))
	}
	if c.opaque != "" {
		parts = append(parts, fmt.Sprintf(`opaque="%s"`, c.opaque))
	}
	return "Digest " + strings.Join(parts, ", ")
}

func newCnonce() string {
	const size = 8
	b := make([]byte, size)
	_, _ = rand.Read(b) // crypto/rand never fails on supported platforms
	return hex.EncodeToString(b)
}
