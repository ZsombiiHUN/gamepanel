package store

import (
	_ "embed"
	"strings"
	"sync"
)

// commonPasswords lists leaked passwords that are long enough to pass the
// length rule, taken from the most common entries of public breach corpora
// (SecLists, "Pwdb top 1,000,000"). Shorter ones are left out because the
// length rule already refuses them. One per line, lower case.
//
//go:embed common_passwords.txt
var commonPasswordsTxt string

var commonPasswords = sync.OnceValue(func() map[string]struct{} {
	m := make(map[string]struct{}, strings.Count(commonPasswordsTxt, "\n")+1)
	for _, l := range strings.Split(commonPasswordsTxt, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			m[strings.ToLower(l)] = struct{}{}
		}
	}
	return m
})

// isCommonPassword reports whether pw, ignoring case, is a known leaked
// password. Lists like this are matched case-insensitively because a capital
// first letter is the most common "variation" people add.
func isCommonPassword(pw string) bool {
	_, ok := commonPasswords()[strings.ToLower(pw)]
	return ok
}
