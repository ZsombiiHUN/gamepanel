package e2e

// Error replies must be readable and must not reveal how the panel is built:
// no file paths, syscall or library messages, and no API description for
// visitors who are not signed in.

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestAPIDescriptionNeedsASignIn(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	anon := &client{t, s.admin.base, &http.Client{}}
	for _, p := range []string{"/api/openapi.json", "/api/openapi.yaml", "/api/docs", "/api/schemas/ErrorModel.json"} {
		if b := anon.do("GET", p, nil, 404); strings.Contains(string(b), "openapi") || strings.Contains(string(b), "paths") {
			t.Errorf("%s leaked to a visitor: %.80s", p, b)
		}
		s.admin.do("GET", p, nil, 200)
	}
}

func TestErrorRepliesRevealNoInternals(t *testing.T) {
	t.Parallel()
	s := newStack(t)

	// A missing file: a plain not-found, not "openat ...: no such file or directory".
	b := s.admin.do("GET", "/api/servers/"+s.server+"/files/content?path=world/level.dat", nil, 404)
	if !strings.Contains(string(b), "no such file or folder") || strings.Contains(string(b), "openat") || strings.Contains(string(b), "level.dat") {
		t.Errorf("missing file: %s", b)
	}
	// Writing over a folder.
	s.write("plugins/a.txt", "x")
	b = s.admin.do("PUT", "/api/servers/"+s.server+"/files/content", map[string]string{"path": "plugins", "content": "x"}, 422)
	if !strings.Contains(string(b), "folder") || strings.Contains(string(b), "is a directory") {
		t.Errorf("write over a folder: %s", b)
	}

	// Error bodies carry no link to the (signed-in only) schema files.
	if b := s.admin.do("POST", "/api/auth/login", map[string]string{"email": "admin@example.com", "password": "wrong-password-here"}, 401); strings.Contains(string(b), "$schema") {
		t.Errorf("schema link in an error body: %s", b)
	}

	// The cross-origin refusal is problem+json with a sentence a person can act on.
	req, _ := http.NewRequest(http.MethodPost, s.admin.base+"/api/auth/login", strings.NewReader(`{}`))
	req.Header.Set("Origin", "https://evil.example")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusForbidden || resp.Header.Get("Content-Type") != "application/problem+json" || !strings.Contains(string(body), "Open the panel directly") {
		t.Errorf("cross-origin reply: %d %s %s", resp.StatusCode, resp.Header.Get("Content-Type"), body)
	}
}
