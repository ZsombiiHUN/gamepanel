package e2e

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/ZsombiiHUN/gamepanel/internal/panel/api"
	"github.com/ZsombiiHUN/gamepanel/internal/version"
)

func get(t *testing.T, url, bearer string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, url, http.NoBody)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func TestHealthAndMetrics(t *testing.T) {
	t.Parallel()
	s := newStackWith(t, func(d *api.Deps) { d.MetricsToken = "metrics-secret" }, nil)
	base := s.admin.base
	if code, body := get(t, base+"/healthz", ""); code != 200 || body != "ok\n" {
		t.Fatalf("healthz: %d %q", code, body)
	}
	if code, body := get(t, base+"/readyz", ""); code != 200 || body != "ok\n" {
		t.Fatalf("readyz: %d %q", code, body)
	}
	// /metrics wants the token, and only the token.
	for _, tok := range []string{"", "wrong", "metrics-secre"} {
		if code, _ := get(t, base+"/metrics", tok); code != 401 {
			t.Fatalf("metrics with %q: %d", tok, code)
		}
	}
	s.admin.do("GET", "/api/servers", nil, 200) // something to count
	code, body := get(t, base+"/metrics", "metrics-secret")
	if code != 200 {
		t.Fatalf("metrics: %d", code)
	}
	for _, want := range []string{
		`gamepanel_build_info{version="` + version.Version + `"} 1`,
		"gamepanel_servers 1\n", "gamepanel_nodes 1\n", "gamepanel_nodes_connected 1\n", "gamepanel_users 1\n",
		`gamepanel_webhook_deliveries{status="pending"} `,
		"gamepanel_database_up 1\n",
		`gamepanel_http_request_duration_seconds_count{area="api",method="GET",code="2xx"} `,
		`gamepanel_http_request_duration_seconds_bucket{area="api",method="GET",code="2xx",le="+Inf"} `,
		"# TYPE gamepanel_http_request_duration_seconds histogram",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("metrics lack %q", want)
		}
	}
	if strings.Contains(body, s.server) || strings.Contains(body, "admin@example.com") {
		t.Error("metrics name a server or an account")
	}

	// The node list says which version each node runs.
	var nodes []struct {
		Version         string `json:"version"`
		VersionMismatch bool   `json:"version_mismatch"`
		Online          bool   `json:"online"`
	}
	json.Unmarshal(s.admin.do("GET", "/api/nodes", nil, 200), &nodes)
	if len(nodes) != 1 || !nodes[0].Online || nodes[0].Version != version.Version || nodes[0].VersionMismatch {
		t.Fatalf("nodes: %+v", nodes)
	}
}

// Without a token, /metrics does not exist.
func TestMetricsOffWithoutToken(t *testing.T) {
	t.Parallel()
	s := newStackWith(t, nil, nil)
	if code, _ := get(t, s.admin.base+"/metrics", ""); code != 404 {
		t.Fatalf("metrics without a configured token: %d", code)
	}
	if code, _ := get(t, s.admin.base+"/metrics", "anything"); code != 404 {
		t.Fatalf("metrics without a configured token, with one sent: %d", code)
	}
}
