package handler_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"

	"github.com/dombyte/pvmon/internal/http/handler"
)

func TestDocs(t *testing.T) {
	t.Parallel()
	built := fstest.MapFS{
		"index.html":   {Data: []byte("swagger")},
		"openapi.yaml": {Data: []byte("spec")},
	}
	cases := []struct {
		name, path, body string
		fsys             fstest.MapFS
		code             int
	}{
		{"index", "/docs/", "swagger", built, http.StatusOK},
		{"file", "/docs/openapi.yaml", "spec", built, http.StatusOK},
		{"unknown", "/docs/nope", "not found", built, http.StatusNotFound},
		{"not built", "/docs/", "make assets", fstest.MapFS{}, http.StatusServiceUnavailable},
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, c.path, nil)
		handler.NewDocs(c.fsys).ServeHTTP(rec, req)
		assert.Equal(t, c.code, rec.Code, c.name)
		assert.Contains(t, rec.Body.String(), c.body, c.name)
	}
}
