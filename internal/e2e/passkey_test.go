package e2e

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fxamacker/cbor/v2"

	"github.com/ZsombiiHUN/gamepanel/internal/panel/api"
	"github.com/ZsombiiHUN/gamepanel/internal/panel/hub"
	"github.com/ZsombiiHUN/gamepanel/internal/panel/store"
)

// authenticator is a software passkey: a P-256 key that signs WebAuthn
// assertions the way a security key or phone does.
type authenticator struct {
	key    *ecdsa.PrivateKey
	credID []byte
	handle []byte // the user handle it stored at registration
	count  uint32
	rpID   string
	origin string
}

var b64 = base64.RawURLEncoding

func newAuthenticator(origin string) *authenticator {
	k, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	id := make([]byte, 16)
	rand.Read(id)
	u, _ := url.Parse(origin)
	return &authenticator{key: k, credID: id, rpID: u.Hostname(), origin: origin}
}

func (a *authenticator) authData(flags byte, attested []byte) []byte {
	h := sha256.Sum256([]byte(a.rpID))
	out := append([]byte{}, h[:]...)
	out = append(out, flags)
	out = binary.BigEndian.AppendUint32(out, a.count)
	return append(out, attested...)
}

func clientData(typ, challenge, origin string) []byte {
	b, _ := json.Marshal(map[string]string{"type": typ, "challenge": challenge, "origin": origin})
	return b
}

// create answers navigator.credentials.create() with "none" attestation.
func (a *authenticator) create(t *testing.T, options json.RawMessage) json.RawMessage {
	var opts struct {
		PublicKey struct {
			Challenge string `json:"challenge"`
			User      struct {
				ID string `json:"id"`
			} `json:"user"`
		} `json:"publicKey"`
	}
	json.Unmarshal(options, &opts)
	a.handle, _ = b64.DecodeString(opts.PublicKey.User.ID)
	cose, _ := cbor.Marshal(map[int]any{1: 2, 3: -7, -1: 1, -2: a.key.X.FillBytes(make([]byte, 32)), -3: a.key.Y.FillBytes(make([]byte, 32))})
	attested := make([]byte, 16) // AAGUID
	attested = binary.BigEndian.AppendUint16(attested, uint16(len(a.credID)))
	attested = append(attested, a.credID...)
	attested = append(attested, cose...)
	// flags: user present, user verified, attested credential data
	att, _ := cbor.Marshal(map[string]any{"fmt": "none", "attStmt": map[string]any{}, "authData": a.authData(0x01|0x04|0x40, attested)})
	resp, _ := json.Marshal(map[string]any{
		"id": b64.EncodeToString(a.credID), "rawId": b64.EncodeToString(a.credID), "type": "public-key",
		"response": map[string]string{"attestationObject": b64.EncodeToString(att), "clientDataJSON": b64.EncodeToString(clientData("webauthn.create", opts.PublicKey.Challenge, a.origin))},
	})
	return resp
}

// get answers navigator.credentials.get(); verified=false leaves out user verification.
func (a *authenticator) get(t *testing.T, options json.RawMessage, verified bool, origin string) json.RawMessage {
	var opts struct {
		PublicKey struct {
			Challenge string `json:"challenge"`
		} `json:"publicKey"`
	}
	json.Unmarshal(options, &opts)
	a.count++
	flags := byte(0x01)
	if verified {
		flags |= 0x04
	}
	ad := a.authData(flags, nil)
	cd := clientData("webauthn.get", opts.PublicKey.Challenge, origin)
	h := sha256.Sum256(cd)
	digest := sha256.Sum256(append(append([]byte{}, ad...), h[:]...))
	sig, _ := ecdsa.SignASN1(rand.Reader, a.key, digest[:])
	resp, _ := json.Marshal(map[string]any{
		"id": b64.EncodeToString(a.credID), "rawId": b64.EncodeToString(a.credID), "type": "public-key",
		"response": map[string]string{"authenticatorData": b64.EncodeToString(ad), "clientDataJSON": b64.EncodeToString(cd),
			"signature": b64.EncodeToString(sig), "userHandle": b64.EncodeToString(a.handle)},
	})
	return resp
}

func passkeyPanel(t *testing.T) (string, *client) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	st, _ := store.Open(filepath.Join(t.TempDir(), "p.db"))
	t.Cleanup(func() { st.Close() })
	st.CreateUser(context.Background(), "admin@example.com", "correct-horse-battery", true)
	ts := httptest.NewUnstartedServer(nil)
	ts.Start()
	t.Cleanup(ts.Close)
	// WebAuthn needs a domain, not an IP address (browsers enforce the same).
	public := strings.Replace(ts.URL, "127.0.0.1", "localhost", 1)
	ts.Config.Handler = api.New(api.Deps{Store: st, Hub: hub.New(log), Log: log, PublicURL: public})
	admin := anon(t, public)
	admin.do("POST", "/api/auth/login", map[string]string{"email": "admin@example.com", "password": "correct-horse-battery"}, 200)
	return public, admin
}

type options struct {
	Options json.RawMessage
	Ticket  string
}

func TestPasskeys(t *testing.T) {
	t.Parallel()
	base, admin := passkeyPanel(t)
	auth := newAuthenticator(base)

	// Adding a passkey asks for the password again.
	admin.do("POST", "/api/auth/passkeys/register/begin", map[string]string{"password": "wrong"}, 403)
	var reg options
	json.Unmarshal(admin.do("POST", "/api/auth/passkeys/register/begin", map[string]string{"password": "correct-horse-battery"}, 200), &reg)
	var pk struct{ ID, Name string }
	json.Unmarshal(admin.do("POST", "/api/auth/passkeys/register/finish", map[string]any{"name": "Laptop", "credential": auth.create(t, reg.Options)}, 201), &pk)
	if pk.Name != "Laptop" {
		t.Fatalf("passkey %+v", pk)
	}
	// The ceremony is single use.
	admin.do("POST", "/api/auth/passkeys/register/finish", map[string]any{"credential": auth.create(t, reg.Options)}, 409)

	// Sign in with it from a fresh browser: no password, no email.
	signIn := func(verified bool, origin string, want int) *client {
		c := anon(t, base)
		var lo options
		json.Unmarshal(c.do("POST", "/api/auth/passkeys/login/begin", nil, 200), &lo)
		c.do("POST", "/api/auth/passkeys/login/finish", map[string]any{"ticket": lo.Ticket, "credential": auth.get(t, lo.Options, verified, origin)}, want)
		return c
	}
	c := signIn(true, base, 200)
	var me struct{ Email string }
	json.Unmarshal(c.do("GET", "/api/auth/me", nil, 200), &me)
	if me.Email != "admin@example.com" {
		t.Fatalf("signed in as %q", me.Email)
	}

	// Refused: no user verification, another site's origin, a replayed answer.
	signIn(false, base, 401)
	signIn(true, "https://evil.example", 401)
	var lo options
	cl := anon(t, base)
	json.Unmarshal(cl.do("POST", "/api/auth/passkeys/login/begin", nil, 200), &lo)
	answer := auth.get(t, lo.Options, true, base)
	cl.do("POST", "/api/auth/passkeys/login/finish", map[string]any{"ticket": lo.Ticket, "credential": answer}, 200)
	cl.do("POST", "/api/auth/passkeys/login/finish", map[string]any{"ticket": lo.Ticket, "credential": answer}, 401)

	// A copied key whose counter goes backwards is refused.
	auth.count = 0
	signIn(true, base, 401)
	auth.count = 100

	// A different key cannot pose as the registered one.
	impostor := newAuthenticator(base)
	impostor.credID, impostor.handle = auth.credID, auth.handle
	json.Unmarshal(cl.do("POST", "/api/auth/passkeys/login/begin", nil, 200), &lo)
	cl.do("POST", "/api/auth/passkeys/login/finish", map[string]any{"ticket": lo.Ticket, "credential": impostor.get(t, lo.Options, true, base)}, 401)

	// Listing, renaming, removing.
	var list []struct {
		ID, Name   string
		LastUsedAt int64 `json:"last_used_at"`
	}
	json.Unmarshal(admin.do("GET", "/api/auth/passkeys", nil, 200), &list)
	if len(list) != 1 || list[0].LastUsedAt == 0 {
		t.Fatalf("list %+v", list)
	}
	admin.do("PUT", "/api/auth/passkeys/"+url.PathEscape(list[0].ID), map[string]string{"name": "Work laptop"}, 204)
	admin.do("DELETE", "/api/auth/passkeys/"+url.PathEscape(list[0].ID), nil, 204)
	signIn(true, base, 401) // removed: no longer signs in
}

func TestPasskeysNeedThePublicAddress(t *testing.T) {
	t.Parallel()
	_, _, base := linkPanel(t, api.Deps{})
	anon(t, base).do("POST", "/api/auth/passkeys/login/begin", nil, 404)
	var opts struct{ Passkeys bool }
	json.Unmarshal(anon(t, base).do("GET", "/api/auth/options", nil, 200), &opts)
	if opts.Passkeys {
		t.Fatal("passkeys offered without a public address")
	}
}
