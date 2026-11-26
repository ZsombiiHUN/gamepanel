package api_test

import (
	"context"
	"testing"

	"github.com/ZsombiiHUN/gamepanel/internal/panel/store"
)

// A key imported from Pterodactyl (panel import-pterodactyl) keeps working
// as it is, in GamePanel's API and in the Pterodactyl client API, so a bot
// set up with it needs no change. A token of that form that was never
// imported does not.
func TestImportedPterodactylKeySignsIn(t *testing.T) {
	t.Parallel()
	e := newEnv(t, "")
	ctx := context.Background()
	u, _ := e.st.UserByEmail(ctx, bobEmail)
	const key = "ptlc_bobsbot0001abcdefghijklmnopqrstuvwxyz012345"
	if _, err := e.st.DB.Exec(`INSERT INTO api_keys(user_id,name,token_hash,prefix,created_at) VALUES(?,?,?,?,1)`,
		u.ID, "Discord bot", store.HashToken(key), key[:16]); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/servers", "/api/client/account"} {
		if code, body := e.client().do("GET", path, nil, map[string]string{"Authorization": "Bearer " + key}); code != 200 {
			t.Errorf("%s with the imported key: %d %s", path, code, body)
		}
	}
	other := "ptlc_bobsbot0002abcdefghijklmnopqrstuvwxyz012345"
	if code, _ := e.client().do("GET", "/api/servers", nil, map[string]string{"Authorization": "Bearer " + other}); code != 401 {
		t.Errorf("a key that was not imported: %d", code)
	}
}
