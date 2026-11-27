package handler

import (
	"io/fs"
	"net/http"
)

// NewDocs serves the embedded API documentation under /docs/, or a plain hint when it was
// not built into this binary.
func NewDocs(fsys fs.FS) http.Handler {
	if _, err := fs.Stat(fsys, indexFile); err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "the API docs are not part of this build: run `make assets` and rebuild",
				http.StatusServiceUnavailable)
		})
	}
	return http.StripPrefix("/docs/", http.FileServerFS(fsys))
}
