package ptero

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
)

// AppKey is Laravel's APP_KEY, which Pterodactyl uses to encrypt two-factor
// secrets, database passwords and node tokens.
type AppKey []byte

// ParseAppKey reads an APP_KEY as it appears in Pterodactyl's .env
// ("base64:..."); a raw 32-byte key is accepted too.
func ParseAppKey(s string) (AppKey, error) {
	s = strings.Trim(strings.TrimSpace(s), `"'`)
	if rest, ok := strings.CutPrefix(s, "base64:"); ok {
		k, err := base64.StdEncoding.DecodeString(rest)
		if err != nil {
			return nil, errors.New("APP_KEY: not valid base64")
		}
		s = string(k)
	}
	switch len(s) {
	case 16, 32: // AES-128-CBC or AES-256-CBC (Pterodactyl's default)
		return AppKey(s), nil
	}
	return nil, errors.New("APP_KEY: must be 16 or 32 bytes, e.g. base64:... from Pterodactyl's .env")
}

var errDecrypt = errors.New("cannot decrypt (wrong APP_KEY?)")

// Decrypt opens a value made by Laravel's encrypt(): base64 of
// {"iv","value","mac"}, AES-CBC with an HMAC-SHA256 over iv+value. The MAC is
// checked before anything is decrypted. A PHP-serialized string (encrypt()'s
// default) is unwrapped.
func (k AppKey) Decrypt(payload string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(payload))
	if err != nil {
		return "", errDecrypt
	}
	var p struct {
		IV    string `json:"iv"`
		Value string `json:"value"`
		MAC   string `json:"mac"`
		Tag   string `json:"tag"`
	}
	if json.Unmarshal(raw, &p) != nil || p.Tag != "" {
		return "", errDecrypt // GCM payloads (a tag) are not what Pterodactyl writes
	}
	m := hmac.New(sha256.New, k)
	m.Write([]byte(p.IV + p.Value))
	want, err := hex.DecodeString(p.MAC)
	if err != nil || !hmac.Equal(m.Sum(nil), want) {
		return "", errDecrypt
	}
	iv, err1 := base64.StdEncoding.DecodeString(p.IV)
	ct, err2 := base64.StdEncoding.DecodeString(p.Value)
	if err1 != nil || err2 != nil || len(iv) != aes.BlockSize || len(ct) == 0 || len(ct)%aes.BlockSize != 0 {
		return "", errDecrypt
	}
	b, err := aes.NewCipher(k)
	if err != nil {
		return "", errDecrypt
	}
	cipher.NewCBCDecrypter(b, iv).CryptBlocks(ct, ct)
	pad := int(ct[len(ct)-1])
	if pad == 0 || pad > aes.BlockSize || pad > len(ct) {
		return "", errDecrypt
	}
	for _, c := range ct[len(ct)-pad:] {
		if int(c) != pad {
			return "", errDecrypt
		}
	}
	return unserializeString(string(ct[:len(ct)-pad])), nil
}

// unserializeString unwraps PHP's serialize() of a string, s:<bytes>:"...";.
// Anything else is returned as it is (encrypt() called without serializing).
func unserializeString(s string) string {
	rest, ok := strings.CutPrefix(s, "s:")
	if !ok {
		return s
	}
	n, rest, ok := strings.Cut(rest, ":")
	size, err := strconv.Atoi(n)
	if !ok || err != nil || size < 0 || len(rest) != size+3 || rest[0] != '"' || rest[size+1:] != `";` {
		return s
	}
	return rest[1 : size+1]
}
