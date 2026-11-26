package web

import (
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestPolicyHashesInlineScripts(t *testing.T) {
	index := []byte("<html><head><script>var a = 1;</script><script type=\"module\" src=\"/assets/x.js\"></script>" +
		"<script>\n  b();\n</script></head></html>")
	p := Policy(index)
	for _, body := range []string{"var a = 1;", "\n  b();\n"} {
		sum := sha256.Sum256([]byte(body))
		if want := "'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'"; !strings.Contains(p, want) {
			t.Errorf("no hash for %q in %s", body, p)
		}
	}
	for _, want := range []string{"script-src 'self' 'sha256-", "object-src 'none'", "base-uri 'none'", "frame-ancestors 'none'", "default-src 'self'"} {
		if !strings.Contains(p, want) {
			t.Errorf("policy lacks %q: %s", want, p)
		}
	}
	for _, bad := range []string{"unsafe-eval", "script-src 'self' 'unsafe-inline'", "script-src *"} {
		if strings.Contains(p, bad) {
			t.Errorf("policy has %q: %s", bad, p)
		}
	}
}

func TestHandlerSendsPolicy(t *testing.T) {
	fsys := fstest.MapFS{
		"index.html":  {Data: []byte("<script>x()</script>")},
		"assets/a.js": {Data: []byte("1")},
	}
	h := handler(fsys)
	for _, path := range []string{"/", "/servers/abc", "/assets/a.js"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, http.NoBody))
		if rec.Code != http.StatusOK || !strings.Contains(rec.Header().Get("Content-Security-Policy"), "'sha256-") {
			t.Errorf("%s: %d, CSP %q", path, rec.Code, rec.Header().Get("Content-Security-Policy"))
		}
	}
}

// The embedded app's inline scripts all have their hash in the policy.
func TestEmbeddedIndex(t *testing.T) {
	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", http.NoBody))
	body := rec.Body.String()
	csp := rec.Header().Get("Content-Security-Policy")
	n := 0
	for _, m := range inlineScript.FindAllStringSubmatch(body, -1) {
		n++
		sum := sha256.Sum256([]byte(m[1]))
		if !strings.Contains(csp, base64.StdEncoding.EncodeToString(sum[:])) {
			t.Errorf("inline script without a hash in the policy: %.60q", m[1])
		}
	}
	if n == 0 {
		t.Skip("the embedded app has no inline script")
	}
}
