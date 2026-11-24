package ptero

import "testing"

// Made by Pterodactyl 1.15.2's own encrypt() with a throwaway key.
const (
	testAppKey   = "base64:7NZw/Mk/56iKLVPFAFqc9/IYSZGf/5uw2tSAlfZ7/Gg="
	totpPayload  = "eyJpdiI6InpKeVJzbnZ0dkpYMHlFcXVOY1RZSnc9PSIsInZhbHVlIjoiOWpnb1RCbnJwRXUrbFU5dklaZ1o1M1V6UmMxVjI2QWNHN2hJMllzUU1iQT0iLCJtYWMiOiI1NjY0MTBlYmMzYmQzOTY1MjNhYWViMmUwZmEyMzg5NTg5N2FiOTBiZTU3MGRjNGFlNzA5OThkMmQwMzkyNGFhIiwidGFnIjoiIn0="
	quotePayload = "eyJpdiI6IitzZzNEMy8rUjZoQWdGVkdFU0huVWc9PSIsInZhbHVlIjoiQm9OU0hjWDJhL0U5dk1DWUZlQ2hOYUduWXN1Y3B5U0hUbEFRVlYrNjYzWT0iLCJtYWMiOiIwMWNlM2IxNWRmNzA5ZDViZWE4OTFkYzNmYmUwYTQ1MWNmMzUyODI1YTAwZmE1MmNmNWQyZTYzMzE4M2VkOWNhIiwidGFnIjoiIn0="
)

func TestDecryptPterodactylValues(t *testing.T) {
	t.Parallel()
	k, err := ParseAppKey(testAppKey)
	if err != nil {
		t.Fatal(err)
	}
	for payload, want := range map[string]string{
		totpPayload:  "JBSWY3DPEHPK3PXP",
		quotePayload: `pässwörd "q" s:3:`,
	} {
		got, err := k.Decrypt(payload)
		if err != nil || got != want {
			t.Errorf("Decrypt = %q, %v; want %q", got, err, want)
		}
	}
}

func TestDecryptRefusesTampering(t *testing.T) {
	t.Parallel()
	k, _ := ParseAppKey(testAppKey)
	other, _ := ParseAppKey("base64:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
	if _, err := other.Decrypt(totpPayload); err == nil {
		t.Error("wrong key accepted")
	}
	for _, p := range []string{"", "not base64!", "e30=", totpPayload[:len(totpPayload)-8]} {
		if _, err := k.Decrypt(p); err == nil {
			t.Errorf("%q accepted", p)
		}
	}
}

func TestParseAppKey(t *testing.T) {
	t.Parallel()
	for _, bad := range []string{"", "base64:!!", "base64:AAAA", "short"} {
		if _, err := ParseAppKey(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	if _, err := ParseAppKey(" \"" + testAppKey + "\" "); err != nil {
		t.Errorf("quoted key from .env refused: %v", err)
	}
}

func TestUnserializeString(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		`s:3:"abc";`:   "abc",
		`s:0:"";`:      "",
		`s:4:"a";b";`:  `a";b`,
		"plain":        "plain",
		`s:9:"short";`: `s:9:"short";`,
		`s:-1:"";`:     `s:-1:"";`,
	} {
		if got := unserializeString(in); got != want {
			t.Errorf("%q -> %q, want %q", in, got, want)
		}
	}
}
