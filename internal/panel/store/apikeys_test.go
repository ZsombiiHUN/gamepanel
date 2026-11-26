package store

import (
	"strings"
	"testing"
)

func TestLooksLikeAPIKey(t *testing.T) {
	t.Parallel()
	secret := strings.Repeat("a1B2", 8) // 32
	for tok, want := range map[string]bool{
		"gp_anything":                   true,
		"ptlc_abcdefghijk" + secret:     true,
		"ptla_abcdefghijk" + secret:     true,
		"abcdefghijklmnop" + secret:     true, // from before Pterodactyl's prefixes
		"ptlc_abcdefghijk" + secret[1:]: false,
		"ptlc_abcdefghij-" + secret:     false,
		"ptlx_abcdefghijk" + secret:     false,
		"":                              false,
		"a-session-cookie-value":        false,
	} {
		if got := LooksLikeAPIKey(tok); got != want {
			t.Errorf("LooksLikeAPIKey(%q) = %v", tok, got)
		}
	}
}
