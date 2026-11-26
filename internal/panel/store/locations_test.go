package store

import (
	"context"
	"errors"
	"testing"
)

func TestLocations(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := open(t)
	eu, err := s.CreateLocation(ctx, " eu ", "Frankfurt")
	if err != nil || eu.Short != "eu" || eu.Long != "Frankfurt" {
		t.Fatalf("create %+v %v", eu, err)
	}
	if _, err := s.CreateLocation(ctx, "EU", ""); !errors.Is(err, ErrLocationTaken) {
		t.Fatalf("a short code differing only in case: %v", err)
	}
	for _, bad := range []string{"", "two\nlines"} {
		if _, err := s.CreateLocation(ctx, bad, ""); err == nil {
			t.Fatalf("short code %q accepted", bad)
		}
	}
	long := "Frankfurt, Germany"
	if l, err := s.UpdateLocation(ctx, eu.ID, nil, &long); err != nil || l.Short != "eu" || l.Long != long {
		t.Fatalf("update %+v %v", l, err)
	}
	us, _ := s.CreateLocation(ctx, "us", "")
	if _, err := s.UpdateLocation(ctx, us.ID, new("eu"), nil); !errors.Is(err, ErrLocationTaken) {
		t.Fatalf("rename onto another location's code: %v", err)
	}

	n, _, _ := s.CreateNode(ctx, "n1", 25565, 25600)
	if err := s.SetNodeLocation(ctx, n.ID, 999); err == nil {
		t.Fatal("a node was put in a location that does not exist")
	}
	if err := s.SetNodeLocation(ctx, n.ID, eu.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetNode(ctx, n.ID); got.LocationID != eu.ID {
		t.Fatalf("node location %d", got.LocationID)
	}
	if err := s.DeleteLocation(ctx, eu.ID); !errors.Is(err, ErrLocationInUse) {
		t.Fatalf("deleting a location with nodes: %v", err)
	}
	if err := s.SetNodeLocation(ctx, n.ID, 0); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteLocation(ctx, eu.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteLocation(ctx, eu.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second delete: %v", err)
	}
}

func TestServerNumbers(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := open(t)
	owner, _ := s.CreateUser(ctx, "o@example.com", "a-long-enough-password", false)
	n, _, _ := s.CreateNode(ctx, "n1", 25565, 25570)
	e, err := s.CreateEgg(ctx, Egg{Name: "e", DockerImage: "i", Startup: "x", Stop: "^C"})
	if err != nil {
		t.Fatal(err)
	}
	egg := e.ID
	mk := func(uuid string) Server {
		t.Helper()
		v, err := s.CreateServer(ctx, Server{UUID: uuid, Name: uuid[:4], OwnerID: owner.ID, NodeID: n.ID, EggID: egg, Limits: Limits{MemoryMB: 512}})
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	a := mk("aaaaaaaa-0000-4000-8000-000000000000")
	b := mk("bbbbbbbb-0000-4000-8000-000000000000")
	ma, _ := s.GetServerMeta(ctx, a.UUID)
	mb, _ := s.GetServerMeta(ctx, b.UUID)
	if ma.ID == 0 || mb.ID != ma.ID+1 || ma.CreatedAt == 0 {
		t.Fatalf("numbers %+v %+v", ma, mb)
	}
	if got, err := s.ServerByNumber(ctx, mb.ID); err != nil || got.UUID != b.UUID {
		t.Fatalf("by number %+v %v", got, err)
	}
	// A deleted server's number is never handed out again.
	if err := s.DeleteServer(ctx, b.UUID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ServerByNumber(ctx, mb.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted server by number: %v", err)
	}
	c := mk("cccccccc-0000-4000-8000-000000000000")
	if mc, _ := s.GetServerMeta(ctx, c.UUID); mc.ID != mb.ID+1 {
		t.Fatalf("number reused: %+v after %+v", mc, mb)
	}
	all, err := s.ServerMetas(ctx)
	if err != nil || len(all) != 2 || all[a.UUID] != ma {
		t.Fatalf("metas %+v %v", all, err)
	}

	// Free ports within requested ranges.
	used, _ := s.UsedPorts(ctx, n.ID)
	if len(used) != 2 || used[a.Port] != a.UUID {
		t.Fatalf("used ports %v", used)
	}
	if p, err := s.FreePortIn(ctx, n.ID, []PortRange{{25569, 25600}, {25500, 25565}}); err != nil || p != 25569 {
		t.Fatalf("free port %d %v", p, err)
	}
	if _, err := s.FreePortIn(ctx, n.ID, []PortRange{{a.Port, a.Port}, {30000, 30010}}); !errors.Is(err, ErrNoPortInRange) {
		t.Fatalf("taken and out-of-range ports: %v", err)
	}
}
