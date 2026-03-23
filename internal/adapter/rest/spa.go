package rest

import (
	"io/fs"
	"net/http"
	"strings"
)

// ServeSPA serves the embedded SPA with fallback to index.html for client-side routing.
func ServeSPA(distFS fs.FS) http.HandlerFunc {
	fileServer := http.FileServerFS(distFS)

	return func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}

		if _, err := fs.Stat(distFS, path); err != nil {
			r.URL.Path = "/"
			path = "index.html"
		}

		fileServer.ServeHTTP(w, r)
	}
}
