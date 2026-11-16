// Package modrinth searches Modrinth (https://modrinth.com) for mods,
// plugins and datapacks, and resolves a version to its download.
package modrinth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Client struct {
	// BaseURL is the API (https://api.modrinth.com/v2); FileHosts the hosts
	// downloads may come from. Tests point both elsewhere.
	BaseURL   string
	FileHosts []string
	// AllowHTTP lets downloads use plain http (tests only).
	AllowHTTP bool
	http      *http.Client
}

func New() *Client {
	return &Client{BaseURL: "https://api.modrinth.com/v2", FileHosts: []string{"cdn.modrinth.com"}, http: &http.Client{Timeout: 15 * time.Second}}
}

// Types this panel can install, and the folder each goes to.
var Folders = map[string]string{"mod": "/mods/", "plugin": "/plugins/", "datapack": "/world/datapacks/"}

type Hit struct {
	ID          string   `json:"project_id"`
	Slug        string   `json:"slug"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Author      string   `json:"author"`
	Downloads   int64    `json:"downloads"`
	IconURL     string   `json:"icon_url"`
	Type        string   `json:"project_type"`
	Versions    []string `json:"versions" doc:"game versions it supports"`
	Categories  []string `json:"categories"`
}

type SearchResult struct {
	Hits  []Hit `json:"hits" nullable:"false"`
	Total int   `json:"total_hits"`
}

func (c *Client) get(ctx context.Context, path string, q url.Values, out any) error {
	u := c.BaseURL + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	// Modrinth asks every client to say who it is.
	req.Header.Set("User-Agent", "ZsombiiHUN/gamepanel (github.com/ZsombiiHUN/gamepanel)")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return ErrNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("modrinth answered %s", resp.Status)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(out)
}

var ErrNotFound = fmt.Errorf("not found on Modrinth")

func facet(vals ...string) string {
	b, _ := json.Marshal(vals)
	return string(b)
}

// Search finds projects of a type, optionally for a loader and game version.
func (c *Client) Search(ctx context.Context, query, typ, loader, version string, offset int) (SearchResult, error) {
	facets := []string{facet("project_type:" + typ)}
	if loader != "" {
		facets = append(facets, facet("categories:"+loader))
	}
	if version != "" {
		facets = append(facets, facet("versions:"+version))
	}
	q := url.Values{"query": {query}, "limit": {"20"}, "offset": {strconv.Itoa(offset)}, "index": {"relevance"},
		"facets": {"[" + strings.Join(facets, ",") + "]"}}
	var r SearchResult
	err := c.get(ctx, "/search", q, &r)
	if r.Hits == nil {
		r.Hits = []Hit{}
	}
	return r, err
}

type File struct {
	URL      string `json:"url"`
	Filename string `json:"filename"`
	Primary  bool   `json:"primary"`
	Size     int64  `json:"size"`
	Hashes   struct {
		SHA512 string `json:"sha512"`
	} `json:"hashes"`
}

type Version struct {
	ID            string   `json:"id"`
	ProjectID     string   `json:"project_id"`
	Name          string   `json:"name"`
	Number        string   `json:"version_number"`
	Type          string   `json:"version_type" doc:"release, beta or alpha"`
	GameVersions  []string `json:"game_versions"`
	Loaders       []string `json:"loaders"`
	DatePublished string   `json:"date_published"`
	Files         []File   `json:"files" nullable:"false"`
}

// Versions lists a project's versions, newest first, optionally filtered.
func (c *Client) Versions(ctx context.Context, project, loader, version string) ([]Version, error) {
	q := url.Values{}
	if loader != "" {
		q.Set("loaders", facet(loader))
	}
	if version != "" {
		q.Set("game_versions", facet(version))
	}
	var vs []Version
	err := c.get(ctx, "/project/"+url.PathEscape(project)+"/version", q, &vs)
	if vs == nil {
		vs = []Version{}
	}
	return vs, err
}

// Download resolves a version to its primary file, refusing files hosted
// anywhere but Modrinth's own CDN.
func (c *Client) Download(ctx context.Context, versionID string) (Version, File, error) {
	var v Version
	if err := c.get(ctx, "/version/"+url.PathEscape(versionID), nil, &v); err != nil {
		return v, File{}, err
	}
	var f *File
	for i := range v.Files {
		if v.Files[i].Primary || f == nil {
			f = &v.Files[i]
		}
	}
	if f == nil {
		return v, File{}, fmt.Errorf("this version has no files")
	}
	u, err := url.Parse(f.URL)
	ok := false
	for _, h := range c.FileHosts {
		ok = ok || (err == nil && u.Host == h)
	}
	if !ok || (u.Scheme != "https" && !(c.AllowHTTP && u.Scheme == "http")) {
		return v, File{}, fmt.Errorf("the download is not on Modrinth's servers")
	}
	if len(f.Hashes.SHA512) != 128 {
		return v, File{}, fmt.Errorf("the download has no checksum")
	}
	return v, *f, nil
}
