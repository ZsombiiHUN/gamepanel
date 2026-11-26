// Package web serves the embedded single-page frontend.
package web

import (
	"crypto/sha256"
	"embed"
	"encoding/base64"
	"io/fs"
	"net/http"
	"path"
	"regexp"
	"strings"
)

//go:embed all:dist
var dist embed.FS

// Handler serves built assets and falls back to index.html for client-side routes.
func Handler() http.Handler {
	sub, _ := fs.Sub(dist, "dist")
	return handler(sub)
}

func handler(sub fs.FS) http.Handler {
	files := http.FileServerFS(sub)
	index, _ := fs.ReadFile(sub, "index.html")
	csp := Policy(index)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if p == "" {
			p = "index.html"
		}
		if _, err := fs.Stat(sub, p); err != nil {
			if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/daemon/") {
				http.NotFound(w, r)
				return
			}
			r.URL.Path = "/" // SPA fallback
		}
		h := w.Header()
		if strings.HasPrefix(p, "assets/") {
			h.Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			h.Set("Cache-Control", "no-cache")
		}
		h.Set("Content-Security-Policy", csp)
		files.ServeHTTP(w, r)
	})
}

var inlineScript = regexp.MustCompile(`(?s)<script>(.*?)</script>`)

// Policy is the Content-Security-Policy for the app whose index.html is
// given. Scripts run only from the panel itself, plus the inline scripts of
// index.html by hash (the theme is picked before the first paint), so text
// that reaches the page from a game server, a file or a mod listing cannot
// run as script even if it slipped past React's escaping.
//
// Styles allow inline: CodeMirror and xterm.js add <style> elements at run
// time. Images may come from any https host (mod icons), data: (server
// icons, the two-factor QR code) and blob:.
func Policy(index []byte) string {
	scripts := []string{"'self'"}
	for _, m := range inlineScript.FindAllSubmatch(index, -1) {
		sum := sha256.Sum256(m[1])
		scripts = append(scripts, "'sha256-"+base64.StdEncoding.EncodeToString(sum[:])+"'")
	}
	return strings.Join([]string{
		"default-src 'self'",
		"script-src " + strings.Join(scripts, " "),
		"style-src 'self' 'unsafe-inline'",
		"img-src 'self' data: blob: https:",
		"font-src 'self'",
		"connect-src 'self'",
		"worker-src 'self' blob:",
		"object-src 'none'",
		"base-uri 'none'",
		"form-action 'self'",
		"frame-ancestors 'none'",
	}, "; ")
}
