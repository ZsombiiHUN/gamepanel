package minecraft

import (
	"context"
	"crypto/md5" //nolint:gosec // G501: Java's offline-mode UUIDs are MD5 name-based (version 3) by definition
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Flavors are the server projects GamePanel's eggs install (the argument of
// gp-install in the egg's install script).
var Flavors = []string{"paper", "purpur", "folia", "vanilla", "fabric", "forge", "neoforge", "velocity", "bungeecord"}

// Sources are the upstream APIs; tests point them at a fake.
type Sources struct {
	Mojang   string // https://piston-meta.mojang.com
	Fill     string // https://fill.papermc.io/v3/projects
	Purpur   string // https://api.purpurmc.org/v2/purpur
	Fabric   string // https://meta.fabricmc.net/v2
	Forge    string // https://files.minecraftforge.net/net/minecraftforge/forge
	NeoForge string // https://maven.neoforged.net/api/maven/versions/releases/net/neoforged/neoforge
	Profiles string // https://api.mojang.com/users/profiles/minecraft
}

var DefaultSources = Sources{
	Mojang:   "https://piston-meta.mojang.com",
	Fill:     "https://fill.papermc.io/v3/projects",
	Purpur:   "https://api.purpurmc.org/v2/purpur",
	Fabric:   "https://meta.fabricmc.net/v2",
	Forge:    "https://files.minecraftforge.net/net/minecraftforge/forge",
	NeoForge: "https://maven.neoforged.net/api/maven/versions/releases/net/neoforged/neoforge",
	Profiles: "https://api.mojang.com/users/profiles/minecraft",
}

const userAgent = "GamePanel/1 (+https://github.com/ZsombiiHUN/gamepanel)"

// Client fetches version lists and player profiles, caching both.
type Client struct {
	Src  Sources
	TTL  time.Duration // how long a version list is kept; 0 = 30 minutes
	http *http.Client

	mu       sync.Mutex
	versions map[string]cached
	profiles map[string]profileHit
}

type cached struct {
	at   time.Time
	list []string
}

type profileHit struct {
	at time.Time
	p  PlayerProfile
	ok bool
}

func NewClient(src Sources) *Client {
	return &Client{Src: src, http: &http.Client{Timeout: 15 * time.Second}, versions: map[string]cached{}, profiles: map[string]profileHit{}}
}

func (c *Client) ttl() time.Duration {
	if c.TTL > 0 {
		return c.TTL
	}
	return 30 * time.Minute
}

func (c *Client) getJSON(ctx context.Context, u string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, http.NoBody)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")
	res, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound || res.StatusCode == http.StatusNoContent {
		return errNotFound
	}
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("%s answered %s", hostOf(u), res.Status)
	}
	return json.NewDecoder(io.LimitReader(res.Body, 16<<20)).Decode(out)
}

var errNotFound = errors.New("not found")

func hostOf(u string) string {
	if p, err := url.Parse(u); err == nil {
		return p.Host
	}
	return "the server"
}

// Versions lists the Minecraft (or, for Velocity, the proxy's own) versions
// a flavor can install, newest first. BungeeCord has only its latest build,
// so its list is empty.
func (c *Client) Versions(ctx context.Context, flavor string) ([]string, error) {
	if !slices.Contains(Flavors, flavor) {
		return nil, fmt.Errorf("unknown server type %q", flavor)
	}
	c.mu.Lock()
	if h, ok := c.versions[flavor]; ok && time.Since(h.at) < c.ttl() {
		c.mu.Unlock()
		return h.list, nil
	}
	c.mu.Unlock()
	list, err := c.fetchVersions(ctx, flavor)
	if err != nil {
		return nil, err
	}
	if len(list) > 400 {
		list = list[:400]
	}
	c.mu.Lock()
	c.versions[flavor] = cached{at: time.Now(), list: list}
	c.mu.Unlock()
	return list, nil
}

func (c *Client) fetchVersions(ctx context.Context, flavor string) ([]string, error) {
	switch flavor {
	case "vanilla":
		var m struct {
			Versions []struct {
				ID   string `json:"id"`
				Type string `json:"type"`
			} `json:"versions"`
		}
		if err := c.getJSON(ctx, c.Src.Mojang+"/mc/game/version_manifest_v2.json", &m); err != nil {
			return nil, err
		}
		var out []string
		for _, v := range m.Versions {
			if v.Type == "release" {
				out = append(out, v.ID)
			}
		}
		return out, nil // the manifest is newest first
	case "paper", "folia", "velocity":
		var r struct {
			Versions []struct {
				Version struct {
					ID string `json:"id"`
				} `json:"version"`
			} `json:"versions"`
		}
		if err := c.getJSON(ctx, c.Src.Fill+"/"+flavor+"/versions", &r); err != nil {
			return nil, err
		}
		var out []string
		for _, v := range r.Versions {
			if v.Version.ID != "" && !strings.Contains(v.Version.ID, "-") {
				out = append(out, v.Version.ID)
			}
		}
		sortDesc(out)
		return out, nil
	case "purpur":
		var r struct {
			Versions []string `json:"versions"`
		}
		if err := c.getJSON(ctx, c.Src.Purpur, &r); err != nil {
			return nil, err
		}
		sortDesc(r.Versions)
		return r.Versions, nil
	case "fabric":
		var r []struct {
			Version string `json:"version"`
			Stable  bool   `json:"stable"`
		}
		if err := c.getJSON(ctx, c.Src.Fabric+"/versions/game", &r); err != nil {
			return nil, err
		}
		var out []string
		for _, v := range r {
			if v.Stable {
				out = append(out, v.Version)
			}
		}
		return out, nil // newest first
	case "forge":
		var r struct {
			Promos map[string]string `json:"promos"`
		}
		if err := c.getJSON(ctx, c.Src.Forge+"/promotions_slim.json", &r); err != nil {
			return nil, err
		}
		seen := map[string]bool{}
		var out []string
		for k := range r.Promos {
			v, _, ok := strings.Cut(k, "-")
			if ok && !seen[v] && releaseRe.MatchString(v) {
				seen[v] = true
				out = append(out, v)
			}
		}
		sortDesc(out)
		return out, nil
	case "neoforge":
		var r struct {
			Versions []string `json:"versions"`
		}
		if err := c.getJSON(ctx, c.Src.NeoForge, &r); err != nil {
			return nil, err
		}
		seen := map[string]bool{}
		var out []string
		for _, nv := range r.Versions {
			if v := neoForgeMinecraft(nv); v != "" && !seen[v] {
				seen[v] = true
				out = append(out, v)
			}
		}
		sortDesc(out)
		return out, nil
	}
	return []string{}, nil // bungeecord: latest only
}

var releaseRe = regexp.MustCompile(`^\d+(\.\d+)+$`)

// neoForgeMinecraft is the Minecraft version a NeoForge version is for:
// 21.1.77 -> 1.21.1, 21.0.5 -> 1.21, 26.3.0.4 -> 26.3, 26.3.1.2 -> 26.3.1
// (the scheme gp-install follows). Betas and odd versions give "".
func neoForgeMinecraft(nv string) string {
	if strings.Contains(nv, "-") {
		return ""
	}
	p := strings.Split(nv, ".")
	if len(p) < 3 {
		return ""
	}
	a, err := strconv.Atoi(p[0])
	if err != nil {
		return ""
	}
	if a < 26 {
		if a < 20 || p[1] == "" {
			return ""
		}
		if p[1] == "0" {
			return "1." + p[0]
		}
		return "1." + p[0] + "." + p[1]
	}
	if p[2] == "0" {
		return p[0] + "." + p[1]
	}
	return p[0] + "." + p[1] + "." + p[2]
}

// sortDesc sorts dotted versions newest first, numerically.
func sortDesc(v []string) {
	slices.SortStableFunc(v, func(a, b string) int { return CompareVersions(b, a) })
}

// CompareVersions compares dotted numeric versions ("1.21.11" > "1.21.2").
func CompareVersions(a, b string) int {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := range max(len(pa), len(pb)) {
		var x, y int
		if i < len(pa) {
			x, _ = strconv.Atoi(pa[i])
		}
		if i < len(pb) {
			y, _ = strconv.Atoi(pb[i])
		}
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}

// PlayerProfile is a player's name and UUID (with dashes).
type PlayerProfile struct {
	Name string `json:"name"`
	UUID string `json:"uuid"`
	// Offline: the UUID is the one an offline-mode server uses, derived
	// from the name, because no Mojang account has this name or the lookup
	// was not asked for.
	Offline bool `json:"offline"`
}

var nameRe = regexp.MustCompile(`^[A-Za-z0-9_]{1,16}$`)

// ValidName reports whether s can be a Minecraft player name.
func ValidName(s string) bool { return nameRe.MatchString(s) }

// ErrNoSuchPlayer means Mojang has no account with that name.
var ErrNoSuchPlayer = errors.New("no Minecraft account has that name")

// Lookup finds a Mojang account by name.
func (c *Client) Lookup(ctx context.Context, name string) (PlayerProfile, error) {
	if !ValidName(name) {
		return PlayerProfile{}, errors.New("a player name is 1 to 16 letters, digits or underscores")
	}
	key := strings.ToLower(name)
	c.mu.Lock()
	if h, ok := c.profiles[key]; ok && time.Since(h.at) < time.Hour {
		c.mu.Unlock()
		if !h.ok {
			return PlayerProfile{}, ErrNoSuchPlayer
		}
		return h.p, nil
	}
	c.mu.Unlock()
	var r struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	err := c.getJSON(ctx, c.Src.Profiles+"/"+url.PathEscape(name), &r)
	if err != nil && !errors.Is(err, errNotFound) {
		return PlayerProfile{}, err
	}
	hit := profileHit{at: time.Now(), ok: err == nil && len(r.ID) == 32}
	if hit.ok {
		hit.p = PlayerProfile{Name: r.Name, UUID: dashed(r.ID)}
	}
	c.mu.Lock()
	if len(c.profiles) > 5000 {
		c.profiles = map[string]profileHit{}
	}
	c.profiles[key] = hit
	c.mu.Unlock()
	if !hit.ok {
		return PlayerProfile{}, ErrNoSuchPlayer
	}
	return hit.p, nil
}

// OfflineUUID is the UUID an offline-mode server gives a name: a version 3
// UUID of "OfflinePlayer:<name>", as Java's UUID.nameUUIDFromBytes makes it.
func OfflineUUID(name string) string {
	h := md5.Sum([]byte("OfflinePlayer:" + name)) //nolint:gosec // G401: see the import
	h[6] = h[6]&0x0f | 0x30
	h[8] = h[8]&0x3f | 0x80
	return dashed(hex.EncodeToString(h[:]))
}

func dashed(h string) string {
	if len(h) != 32 {
		return h
	}
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}
