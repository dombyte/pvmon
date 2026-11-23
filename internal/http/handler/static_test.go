package handler_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"

	"github.com/dombyte/pvmon/internal/http/handler"
)

func TestStatic(t *testing.T) {
	t.Parallel()
	built := fstest.MapFS{
		"index.html":           {Data: []byte("<html>ui</html>")},
		"assets/app-1.js":      {Data: []byte("js")},
		"sw.js":                {Data: []byte("worker")},
		"manifest.webmanifest": {Data: []byte("{}")},
		"icon.svg":             {Data: []byte("<svg/>")},
	}
	tests := []struct {
		name, path, body, cache string
		fsys                    fstest.MapFS
		code                    int
	}{
		{"root", "/", "<html>ui</html>", "no-cache", built, 200},
		{"client route", "/history/day", "<html>ui</html>", "no-cache", built, 200},
		{"index direct", "/index.html", "<html>ui</html>", "no-cache", built, 200},
		{"hashed asset", "/assets/app-1.js", "js", "public, max-age=31536000, immutable", built, 200},
		{"service worker", "/sw.js", "worker", "no-cache", built, 200},
		{"manifest", "/manifest.webmanifest", "{}", "no-cache", built, 200},
		{"plain file", "/icon.svg", "<svg/>", "", built, 200},
		{"directory", "/assets", "<html>ui</html>", "no-cache", built, 200},
		{"unknown api", "/api/v1/nope", `"code":"not_found"`, "no-store", built, 404},
		{"not built", "/", "make assets", "", fstest.MapFS{}, 503},
		{"broken index", "/", "Internal", "", fstest.MapFS{"index.html": {Mode: 0o20000000000}}, 500},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rec := httptest.NewRecorder()
			handler.NewStatic(tt.fsys, zerolog.Nop()).
				ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tt.path, nil))
			assert.Equal(t, tt.code, rec.Code)
			assert.Contains(t, rec.Body.String(), tt.body)
			assert.Equal(t, tt.cache, rec.Header().Get("Cache-Control"))
		})
	}
	rec := httptest.NewRecorder()
	handler.NewStatic(built, zerolog.Nop()).
		ServeHTTP(rec, httptest.NewRequest(http.MethodHead, "/", nil))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Empty(t, rec.Body.String())
}
