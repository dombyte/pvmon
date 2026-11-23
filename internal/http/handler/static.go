package handler

import (
	"errors"
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/rs/zerolog"
)

const (
	indexFile  = "index.html"
	workerFile = "sw.js"
	manifest   = "manifest.webmanifest"
	assetsDir  = "assets/"
	cacheAsset = "public, max-age=31536000, immutable" // file names carry a content hash
	cacheIndex = "no-cache"
)

// Static serves the embedded single-page UI: existing files as they are, every other
// path as index.html so client-side routes survive a reload. Unknown /api/ paths get the
// JSON 404 instead of the page.
type Static struct {
	fsys fs.FS
	srv  http.Handler
	log  zerolog.Logger
}

// NewStatic builds the handler for the UI files in fsys.
func NewStatic(fsys fs.FS, log zerolog.Logger) *Static {
	return &Static{fsys: fsys, srv: http.FileServerFS(fsys), log: log}
}

// ServeHTTP serves a file or the page.
func (s *Static) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		writeJSON(w, http.StatusNotFound, errorBody{
			Error: http.StatusText(http.StatusNotFound), Code: "not_found",
			Message: "unknown endpoint",
		}, s.log)
		return
	}
	name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if name != "" && name != indexFile && s.exists(name) {
		if cc := cacheControl(name); cc != "" {
			w.Header().Set("Cache-Control", cc)
		}
		s.srv.ServeHTTP(w, r)
		return
	}
	s.index(w, r)
}

// cacheControl is the header for a file: hashed assets forever, the worker and the
// manifest always revalidated (a new worker is how a new UI version gets installed).
func cacheControl(name string) string {
	switch {
	case strings.HasPrefix(name, assetsDir):
		return cacheAsset
	case name == workerFile || name == manifest:
		return cacheIndex
	default:
		return ""
	}
}

func (s *Static) exists(name string) bool {
	st, err := fs.Stat(s.fsys, name)
	return err == nil && !st.IsDir()
}

// index serves index.html, or a plain hint when the UI was not built into this binary.
func (s *Static) index(w http.ResponseWriter, r *http.Request) {
	data, err := fs.ReadFile(s.fsys, indexFile)
	if errors.Is(err, fs.ErrNotExist) {
		http.Error(w, "the web UI is not part of this build: run `make assets` and rebuild",
			http.StatusServiceUnavailable)
		return
	}
	if err != nil {
		s.log.Error().Err(err).Msg("read index.html")
		http.Error(w, http.StatusText(http.StatusInternalServerError),
			http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", cacheIndex)
	if r.Method == http.MethodHead {
		return
	}
	if _, err := w.Write(data); err != nil {
		s.log.Debug().Err(err).Msg("write index.html")
	}
}
