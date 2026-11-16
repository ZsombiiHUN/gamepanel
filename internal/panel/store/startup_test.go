package store

import "testing"

func TestValidateVar(t *testing.T) {
	cases := []struct {
		rules, val string
		ok         bool
	}{
		{"required|string|max:20", "", false},
		{"required|string|max:20", "paper.jar", true},
		{"required|string|max:5", "toolong", false},
		{"nullable|string|max:5", "", true},
		{"required|integer|between:1,64", "64", true},
		{"required|integer|between:1,64", "65", false},
		{"required|integer|between:1,64", "1.5", false},
		{"required|integer", "abc", false},
		{"required|numeric|min:0.5", "0.75", true},
		{"required|numeric|min:0.5", "0.25", false},
		{"required|boolean", "true", true},
		{"required|boolean", "1", true},
		{"required|boolean", "yes", false},
		{"required|string|in:survival,creative", "creative", true},
		{"required|string|in:survival,creative", "hardcore", false},
		{"required|regex:/^([\\w\\d._-]+)(\\.jar)$/", "server.jar", true},
		{"required|regex:/^([\\w\\d._-]+)(\\.jar)$/", "server.zip", false},
		{"required|regex:/^(latest|1\\.\\d+)$/|max:10", "latest", true}, // "|" inside the regex
		{"required|regex:/^(latest|1\\.\\d+)$/|max:10", "nope", false},
		{"required|regex:/(?=lookahead)/", "anything", true}, // Go cannot compile it: ignored
		{"required|alpha_dash", "my-world_1", true},
		{"required|alpha_dash", "my world", false},
		{"required|url", "https://example.com/pack.zip", true},
		{"required|url", "example.com", false},
		{"required|some_rule_we_do_not_know:1", "x", true},
		{"", "anything at all", true},
	}
	for _, c := range cases {
		err := ValidateVar(EggVar{Name: "V", Env: "V", Rules: c.rules}, c.val)
		if (err == nil) != c.ok {
			t.Errorf("rules %q value %q: got err=%v, want ok=%v", c.rules, c.val, err, c.ok)
		}
	}
	if ValidateVar(EggVar{Name: "V"}, "a\x00b") == nil {
		t.Error("a NUL byte was accepted")
	}
	long := make([]byte, MaxVarLen+1)
	for i := range long {
		long[i] = 'a'
	}
	if ValidateVar(EggVar{Name: "V"}, string(long)) == nil {
		t.Error("an over-long value was accepted")
	}
}

func TestVarRulesSummary(t *testing.T) {
	if got := VarRulesSummary("required|integer|between:1,64"); got != "required, whole number, 1 to 64" {
		t.Fatalf("%q", got)
	}
}
