// Package web embeds the compiled RSCON web console.
//
// The console is a static React application built from web/ into dist/.
// The binary always ships with an embedded console; there is no runtime
// dependency on a Node.js toolchain.
package web

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed all:dist
var assets embed.FS

// Handler returns an http.Handler serving the embedded console. Unknown
// non-asset paths fall back to index.html so client-side routing works.
func Handler() (http.Handler, error) {
	sub, err := fs.Sub(assets, "dist")
	if err != nil {
		return nil, err
	}
	fileServer := http.FileServer(http.FS(sub))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if path == "" || path == "/" {
			fileServer.ServeHTTP(w, r)
			return
		}

		f, err := sub.Open(path[1:])
		if err == nil {
			_ = f.Close()
			fileServer.ServeHTTP(w, r)
			return
		}

		// SPA fallback.
		r2 := r.Clone(r.Context())
		r2.URL.Path = "/"
		fileServer.ServeHTTP(w, r2)
	}), nil
}
