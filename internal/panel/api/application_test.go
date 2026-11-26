package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/ZsombiiHUN/gamepanel/internal/panel/store"
)

func TestFlexDecoding(t *testing.T) {
	t.Parallel()
	var in struct {
		A, B, C, D, E flexInt
		Missing       flexInt
		T1, T2, T3    looseBool
		S1, S2, S3    flexString
		O1, O2        optString
		Absent        optString
	}
	body := `{"A":512,"B":"1024","C":null,"D":"","E":2.0,"T1":"1","T2":false,"T3":"true",
		"S1":42,"S2":true,"S3":null,"O1":null,"O2":"0-1"}`
	if err := readJSON(httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body)), &in); err != nil {
		t.Fatal(err)
	}
	if in.A.V != 512 || !in.A.Set || in.B.V != 1024 || !in.C.Null || in.C.Set || !in.D.Null || in.E.V != 2 || in.Missing.Set || in.Missing.Null {
		t.Fatalf("ints %+v %+v %+v %+v %+v %+v", in.A, in.B, in.C, in.D, in.E, in.Missing)
	}
	if in.C.or(7) != 7 || in.A.or(7) != 512 {
		t.Fatal("or")
	}
	if !in.T1.V || in.T2.V || !in.T2.Set || !in.T3.V {
		t.Fatalf("bools %+v %+v %+v", in.T1, in.T2, in.T3)
	}
	if in.S1 != "42" || in.S2 != "1" || in.S3 != "" {
		t.Fatalf("strings %q %q %q", in.S1, in.S2, in.S3)
	}
	if !in.O1.Set || in.O1.V != "" || in.O2.V != "0-1" || in.Absent.Set {
		t.Fatalf("optional strings %+v %+v %+v", in.O1, in.O2, in.Absent)
	}
}

func TestReadJSONNamesTheBadField(t *testing.T) {
	t.Parallel()
	for body, want := range map[string]string{
		`{"user":"abc"}`:                        "user",
		`{"limits":{"memory":"lots"}}`:          "limits.memory",
		`{"memory":1.5}`:                        "memory",
		`{"add_allocations":[1,"x"]}`:           "add_allocations.1",
		`{"start_on_completion":"maybe"}`:       "start_on_completion",
		`{"feature_limits":{"backups":[1]}}`:    "feature_limits.backups",
		`{"allocation":{"default":"25565:12"}}`: "allocation.default",
	} {
		// What a create or a build change would make of it.
		var create appCreateIn
		var build appBuildIn
		err := readJSON(httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body)), &create)
		if err == nil {
			err = readJSON(httptest.NewRequest(http.MethodPatch, "/", strings.NewReader(body)), &build)
		}
		var fe *fieldError
		if !errors.As(err, &fe) || fe.Field != want {
			t.Errorf("%s: %v, want field %q", body, err, want)
		}
	}
	var in appCreateIn
	if err := readJSON(httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{not json`)), &in); err == nil {
		t.Fatal("bad JSON accepted")
	}
	if err := readJSON(httptest.NewRequest(http.MethodPost, "/", strings.NewReader(``)), &in); err != nil {
		t.Fatalf("empty body: %v", err)
	}
}

func TestQueryList(t *testing.T) {
	t.Parallel()
	for raw, want := range map[string][]string{
		"include=allocations,variables":             {"allocations", "variables"},
		"include[]=a&include[]=b":                   {"a", "b"},
		"include%5B0%5D=a&include%5B1%5D=b":         {"a", "b"},
		"location_ids=1&includes=x&other[0]=y":      nil,
		"include=&include= allocations ,":           {"allocations"},
		"location_ids[0]=3&location_ids[1]=4,5":     nil,
		"location_ids%5B%5D=3&location_ids%5B%5D=4": nil,
	} {
		q, _ := url.ParseQuery(raw)
		got := queryList(q, "include")
		slices.Sort(got)
		if !slices.Equal(got, want) {
			t.Errorf("%s: %q, want %q", raw, got, want)
		}
	}
	q, _ := url.ParseQuery("location_ids[0]=3&location_ids[1]=4,5")
	if got := queryList(q, "location_ids"); len(got) != 3 {
		t.Fatalf("location ids %q", got)
	}
}

func TestPortRanges(t *testing.T) {
	t.Parallel()
	got, err := portRanges([]flexString{"25565", " 25600 - 25610 ", "", "30000-30000"})
	if err != nil || !slices.Equal(got, []store.PortRange{{From: 25565, To: 25565}, {From: 25600, To: 25610}, {From: 30000, To: 30000}}) {
		t.Fatalf("%v %v", got, err)
	}
	for _, bad := range []flexString{"abc", "9-1", "0", "70000", "1-2-3"} {
		if _, err := portRanges([]flexString{bad}); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestAllocationIDs(t *testing.T) {
	t.Parallel()
	id := allocID(3, 25565)
	if n, p, ok := splitAllocID(id); !ok || n != 3 || p != 25565 {
		t.Fatalf("%d -> %d %d %v", id, n, p, ok)
	}
	for _, bad := range []int64{0, 25565, 3 << 16, -1} {
		if _, _, ok := splitAllocID(bad); ok {
			t.Errorf("%d accepted", bad)
		}
	}
}

func TestAppLimits(t *testing.T) {
	t.Parallel()
	cur := store.Limits{MemoryMB: 1024, SwapMB: -1, DiskMB: 5000, CPUPct: 100, CPUSet: "0-1", IOWeight: 0, BackupLimit: 3, PortLimit: 2, DatabaseLimit: 1}
	// Fields left out keep their value; io is never taken.
	l, err := appLimitsIn{Memory: flexInt{V: 2048, Set: true}, IO: flexInt{V: 500, Set: true}}.apply(cur)
	if err != nil || l.MemoryMB != 2048 || l.SwapMB != -1 || l.DiskMB != 5000 || l.CPUSet != "0-1" || l.IOWeight != 0 {
		t.Fatalf("%+v %v", l, err)
	}
	if l, _ := (appLimitsIn{Threads: optString{Set: true}}).apply(cur); l.CPUSet != "" {
		t.Fatalf("null threads unpins: %q", l.CPUSet)
	}
	for _, m := range []int64{0, 63} {
		if _, err := (appLimitsIn{Memory: flexInt{V: m, Set: true}}).apply(cur); err == nil {
			t.Errorf("memory %d accepted", m)
		}
	}
	f := appFeatureLimitsIn{Databases: flexInt{Null: true}, Allocations: flexInt{V: 1, Set: true}, Backups: flexInt{V: 0, Set: true}}.apply(cur)
	if f.DatabaseLimit != 0 || f.PortLimit != 0 || f.BackupLimit != 1 {
		t.Fatalf("feature limits %+v", f)
	}
	if f := (appFeatureLimitsIn{}).apply(cur); f != cur {
		t.Fatalf("nothing sent changed %+v", f)
	}
}

// Every badDecoder keeps its flag in a bool field named bad, which badField reads.
func TestBadDecodersHaveABadField(t *testing.T) {
	t.Parallel()
	for _, v := range []badDecoder{flexInt{}, looseBool{}} {
		f, ok := reflect.TypeOf(v).FieldByName("bad")
		if !ok || f.Type.Kind() != reflect.Bool {
			t.Errorf("%T has no bool field named bad", v)
		}
	}
}

func TestHasRoom(t *testing.T) {
	t.Parallel()
	untracked := store.Node{}
	if !hasRoom(untracked, 1<<20, 0) {
		t.Fatal("a node that tracks nothing has room for anything")
	}
	n := store.Node{MemoryMB: 1000, DiskMB: 1000, OverallocatePct: 50, AllocatedMemory: 900, AllocatedDisk: 900}
	for _, c := range []struct {
		mem, disk int64
		want      bool
	}{{600, 600, true}, {601, 100, false}, {100, 601, false}, {100, 0, false}} {
		if got := hasRoom(n, c.mem, c.disk); got != c.want {
			t.Errorf("mem %d disk %d: %v", c.mem, c.disk, got)
		}
	}
}
