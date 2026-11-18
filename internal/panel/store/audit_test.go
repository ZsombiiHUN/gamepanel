package store

import (
	"context"
	"testing"
)

func seedAudit(t *testing.T, s *Store) {
	t.Helper()
	ctx := context.Background()
	for _, e := range []AuditEntry{
		{ActorEmail: "admin@example.com", Action: "login.ok", Target: "admin@example.com", IP: "10.0.0.1"},
		{ActorEmail: "bob@example.com", Action: "login.fail", Target: "bob@example.com", IP: "203.0.113.9", Detail: "bad second factor"},
		{ActorEmail: "admin@example.com", Action: "server.create", Target: "abc-123", Detail: "Survival SMP"},
		{ActorEmail: "admin@example.com", Action: "node.capacity", Target: "1", Detail: "mem=100% disk=_x"},
	} {
		s.Audit(ctx, e)
	}
}

func actions(es []AuditEntry) []string {
	out := []string{}
	for _, e := range es {
		out = append(out, e.Action)
	}
	return out
}

func TestListAuditSearch(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := open(t)
	seedAudit(t, s)

	cases := []struct {
		name string
		q    string
		want []string // newest first
	}{
		{"empty query returns everything", "", []string{"node.capacity", "server.create", "login.fail", "login.ok"}},
		{"whitespace only returns everything", "   ", []string{"node.capacity", "server.create", "login.fail", "login.ok"}},
		{"matches action", "login", []string{"login.fail", "login.ok"}},
		{"case insensitive", "LOGIN.FAIL", []string{"login.fail"}},
		{"matches user", "bob@", []string{"login.fail"}},
		{"matches ip", "203.0.113", []string{"login.fail"}},
		{"matches detail", "survival", []string{"server.create"}},
		{"matches target", "abc-123", []string{"server.create"}},
		{"every word must match", "login admin", []string{"login.ok"}},
		{"words may hit different columns", "survival admin", []string{"server.create"}},
		{"no match", "zzz", []string{}},
		{"percent is literal", "100%", []string{"node.capacity"}},
		{"bare percent does not match everything", "%", []string{"node.capacity"}},
		{"underscore is literal", "_x", []string{"node.capacity"}},
		{"backslash is literal", `\`, []string{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := s.ListAudit(ctx, c.q, 100, 0)
			if err != nil {
				t.Fatal(err)
			}
			g := actions(got)
			if len(g) != len(c.want) {
				t.Fatalf("q=%q got %v want %v", c.q, g, c.want)
			}
			for i := range g {
				if g[i] != c.want[i] {
					t.Fatalf("q=%q got %v want %v", c.q, g, c.want)
				}
			}
		})
	}
}

func TestListAuditSearchPaginates(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := open(t)
	seedAudit(t, s)
	page, err := s.ListAudit(ctx, "example.com", 2, 0)
	if err != nil || len(page) != 2 {
		t.Fatalf("page 1: %v %v", err, actions(page))
	}
	rest, err := s.ListAudit(ctx, "example.com", 2, 2)
	if err != nil || len(rest) != 2 {
		t.Fatalf("page 2: %v %v", err, actions(rest))
	}
	if page[1].ID <= rest[0].ID {
		t.Fatal("pages overlap or are out of order")
	}
}

func TestListAuditSearchIsNotInjectable(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := open(t)
	seedAudit(t, s)
	got, err := s.ListAudit(ctx, `x' OR '1'='1`, 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("injection matched rows: %v", actions(got))
	}
}

func TestLabels(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := open(t)
	eggs, nodes, err := s.Labels(ctx)
	if err != nil || len(eggs) != 0 || len(nodes) != 0 {
		t.Fatalf("empty db: %v %v %v", eggs, nodes, err)
	}
	n, _, err := s.CreateNode(ctx, "eu-west-1", 25565, 25570)
	if err != nil {
		t.Fatal(err)
	}
	e, err := s.CreateEgg(ctx, Egg{Name: "Paper Minecraft", DockerImage: "img", Startup: "run", Stop: "stop"})
	if err != nil {
		t.Fatal(err)
	}
	eggs, nodes, err = s.Labels(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if eggs[e.ID] != "Paper Minecraft" || nodes[n.ID] != "eu-west-1" || len(eggs) != 1 || len(nodes) != 1 {
		t.Fatalf("labels wrong: %v %v", eggs, nodes)
	}
}
