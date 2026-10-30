package web

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed dist
var embedded embed.FS

func Handler() http.Handler {
	assets, err := fs.Sub(embedded, "dist")
	if err != nil {
		panic(err)
	}
	files := http.FileServer(http.FS(assets))

	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requestPath := path.Clean("/" + request.URL.Path)
		if requestPath == "/api" || strings.HasPrefix(requestPath, "/api/") {
			http.NotFound(writer, request)
			return
		}

		assetPath := strings.TrimPrefix(requestPath, "/")
		if assetPath == "." || assetPath == "" {
			assetPath = "index.html"
		}
		if info, err := fs.Stat(assets, assetPath); err == nil && !info.IsDir() {
			files.ServeHTTP(writer, request)
			return
		}
		if assetPath == "assets" || strings.HasPrefix(assetPath, "assets/") || assetPath == "favicon.ico" {
			http.NotFound(writer, request)
			return
		}

		fallback := request.Clone(request.Context())
		urlCopy := *request.URL
		urlCopy.Path = "/"
		fallback.URL = &urlCopy
		files.ServeHTTP(writer, fallback)
	})
}
