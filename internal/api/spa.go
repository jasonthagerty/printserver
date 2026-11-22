package api

import (
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// spaHandler serves the built GUI. A path naming a file is served as-is. Any
// other path gets index.html so client-side routes survive a reload, except
// under assets/, where a miss is a real 404 (a stale bundle after a deploy
// must fail visibly, not load HTML as JavaScript).
func spaHandler(webFS fs.FS) http.Handler {
	files := http.FileServerFS(webFS)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		info, err := fs.Stat(webFS, name)
		switch {
		case err == nil && !info.IsDir():
			if strings.HasPrefix(name, "assets/") {
				// Vite content-hashes asset names, so they never change.
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			}
			files.ServeHTTP(w, r)
		case strings.HasPrefix(name, "assets/"):
			http.NotFound(w, r)
		default:
			w.Header().Set("Cache-Control", "no-cache")
			http.ServeFileFS(w, r, webFS, "index.html")
		}
	})
}
