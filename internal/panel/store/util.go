package store

import (
	"crypto/subtle"
	"encoding/hex"
	"strings"
)

func hexDecode(s string) ([]byte, error)        { return hex.DecodeString(s) }
func constEq(a, b []byte) bool                  { return subtle.ConstantTimeCompare(a, b) == 1 }
func splitN(s string, sep byte, n int) []string { return strings.SplitN(s, string(sep), n) }
