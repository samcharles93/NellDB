package server

import (
	"embed"
	"net/http"
)

//go:embed webui/index.html webui/dist/nell.wasm webui/dist/nell.js webui/dist/wasm_exec.js
var webUIContent embed.FS

// WebUIHandler returns an http.Handler that serves the embedded Web UI and SDK.
func WebUIHandler() http.Handler {
	// We use a custom handler to map the files to the /ui/ path
	mux := http.NewServeMux()
	
	// index.html at root
	mux.HandleFunc("/index.html", func(w http.ResponseWriter, r *http.Request) {
		b, _ := webUIContent.ReadFile("webui/index.html")
		w.Header().Set("Content-Type", "text/html")
		w.Write(b)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" || r.URL.Path == "" {
			http.Redirect(w, r, "/ui/index.html", http.StatusMovedPermanently)
			return
		}
		// Try to serve from webui/dist/
		path := r.URL.Path
		var content []byte
		var contentType string

		switch path {
		case "/nell.wasm":
			content, _ = webUIContent.ReadFile("webui/dist/nell.wasm")
			contentType = "application/wasm"
		case "/nell.js":
			content, _ = webUIContent.ReadFile("webui/dist/nell.js")
			contentType = "application/javascript"
		case "/wasm_exec.js":
			content, _ = webUIContent.ReadFile("webui/dist/wasm_exec.js")
			contentType = "application/javascript"
		}

		if content != nil {
			w.Header().Set("Content-Type", contentType)
			w.Write(content)
			return
		}
		http.NotFound(w, r)
	})
	
	return mux
}
