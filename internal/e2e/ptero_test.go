package e2e

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"testing"
)

// ptero is a client the way a Discord bot talks to Pterodactyl: a bearer key, no cookies.
type ptero struct {
	t    *testing.T
	base string
	key  string
}

func (p ptero) do(method, path string, body any, want int) []byte {
	p.t.Helper()
	var r io.Reader
	if s, ok := body.(string); ok {
		r = strings.NewReader(s)
	} else if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, p.base+path, r)
	req.Header.Set("Authorization", "Bearer "+p.key)
	req.Header.Set("Accept", "Application/vnd.pterodactyl.v1+json")
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		p.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != want {
		p.t.Fatalf("%s %s: got %d want %d: %s", method, path, resp.StatusCode, want, b)
	}
	return b
}

func TestPterodactylClientAPI(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	p := ptero{t, s.admin.base, s.apiKey(s.admin, "correct-horse-battery")}
	short := s.server[:8]

	var acct struct {
		Object     string
		Attributes struct{ Email string }
	}
	json.Unmarshal(p.do("GET", "/api/client/account", nil, 200), &acct)
	if acct.Object != "user" || acct.Attributes.Email != "admin@example.com" {
		t.Fatalf("account %+v", acct)
	}
	var list struct {
		Object string
		Data   []struct {
			Object     string
			Attributes struct {
				Identifier, UUID string
				Limits           struct{ Memory int64 }
				Relationships    struct {
					Allocations struct {
						Data []struct {
							Attributes struct {
								Port      int
								IsDefault bool `json:"is_default"`
							}
						}
					}
				}
			}
		}
	}
	json.Unmarshal(p.do("GET", "/api/client", nil, 200), &list)
	if list.Object != "list" || len(list.Data) != 1 || list.Data[0].Attributes.Identifier != short || list.Data[0].Attributes.Limits.Memory != 256 {
		t.Fatalf("list %+v", list)
	}
	if a := list.Data[0].Attributes.Relationships.Allocations.Data; len(a) != 1 || a[0].Attributes.Port != 25565 || !a[0].Attributes.IsDefault {
		t.Fatalf("allocations %+v", a)
	}

	// Power, command and resources, by the short identifier.
	p.do("POST", "/api/client/servers/"+short+"/power", map[string]string{"signal": "explode"}, 422)
	p.do("POST", "/api/client/servers/"+short+"/command", map[string]string{"command": "say hi"}, 502) // not running
	p.do("POST", "/api/client/servers/"+short+"/power", map[string]string{"signal": "start"}, 204)
	waitFor(t, "running", func() bool { return s.state() == "running" })
	p.do("POST", "/api/client/servers/"+short+"/command", map[string]string{"command": "say hi"}, 204)
	var res struct {
		Attributes struct {
			CurrentState string `json:"current_state"`
		}
	}
	json.Unmarshal(p.do("GET", "/api/client/servers/"+s.server+"/resources", nil, 200), &res)
	if res.Attributes.CurrentState != "running" {
		t.Fatalf("resources %+v", res)
	}

	// Files.
	p.do("POST", "/api/client/servers/"+short+"/files/write?file=/server.properties", "motd=hello", 204)
	if b := string(p.do("GET", "/api/client/servers/"+short+"/files/contents?file=/server.properties", nil, 200)); b != "motd=hello" {
		t.Fatalf("contents %q", b)
	}
	var files struct {
		Data []struct {
			Attributes struct {
				Name   string
				IsFile bool `json:"is_file"`
			}
		}
	}
	json.Unmarshal(p.do("GET", "/api/client/servers/"+short+"/files/list?directory=/", nil, 200), &files)
	if len(files.Data) == 0 || files.Data[0].Attributes.Name != "server.properties" || !files.Data[0].Attributes.IsFile {
		t.Fatalf("files %+v", files)
	}
	p.do("GET", "/api/client/servers/"+short+"/backups", nil, 200)

	// Errors come in Pterodactyl's shape; strangers and bad keys get nothing.
	var e struct {
		Errors []struct{ Code string }
	}
	json.Unmarshal(p.do("GET", "/api/client/servers/deadbeef", nil, 404), &e)
	if len(e.Errors) != 1 || e.Errors[0].Code != "NotFoundHttpException" {
		t.Fatalf("error %+v", e)
	}
	ptero{t, s.admin.base, "gp_wrong"}.do("GET", "/api/client", nil, 401)
	s.st.CreateUser(s.ctx, "bob@example.com", "another-long-password", false)
	jar, _ := cookiejar.New(nil)
	bob := &client{t, s.admin.base, &http.Client{Jar: jar}}
	bob.do("POST", "/api/auth/login", map[string]string{"email": "bob@example.com", "password": "another-long-password"}, 200)
	bp := ptero{t, s.admin.base, s.apiKey(bob, "another-long-password")}
	bp.do("GET", "/api/client/servers/"+short, nil, 404)
	s.admin.do("POST", "/api/servers/"+s.server+"/users", map[string]any{"email": "bob@example.com", "permissions": []string{"console.read"}}, 201)
	bp.do("GET", "/api/client/servers/"+short+"/resources", nil, 200)
	bp.do("POST", "/api/client/servers/"+short+"/power", map[string]string{"signal": "stop"}, 403)
}
