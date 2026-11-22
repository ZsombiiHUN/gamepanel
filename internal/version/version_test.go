package version

import (
	"strings"
	"testing"
)

func TestString(t *testing.T) {
	old := Version
	t.Cleanup(func() { Version, Commit, Date = old, "", "" })
	Version, Commit, Date = "v1.2.3", "0123456789abcdef0123", "2026-11-22T10:00:00Z"
	got := String("panel")
	for _, want := range []string{"panel v1.2.3 ", "commit 0123456789ab,", "built 2026-11-22,"} {
		if !strings.Contains(got, want) {
			t.Errorf("String() = %q, want it to contain %q", got, want)
		}
	}
}

func TestIsFlag(t *testing.T) {
	for _, a := range []string{"version", "--version", "-version", "-v"} {
		if !IsFlag(a) {
			t.Errorf("IsFlag(%q) = false", a)
		}
	}
	if IsFlag("serve") {
		t.Error(`IsFlag("serve") = true`)
	}
}
