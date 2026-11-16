// Package s3 is the small part of the S3 API the panel needs for backups:
// multipart uploads through presigned URLs, presigned downloads, and delete.
// It signs with AWS Signature Version 4 and works with AWS, MinIO, Cloudflare
// R2, Backblaze B2, Wasabi and other S3-compatible stores.
//
// Credentials never leave the panel: nodes only ever receive presigned URLs
// that each allow one request on one object for a short time.
package s3

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Endpoint  string // https://s3.eu-central-1.amazonaws.com, http://minio:9000
	Region    string // us-east-1 when the store does not care
	Bucket    string
	AccessKey string
	SecretKey string
	Prefix    string // folder inside the bucket, e.g. "gamepanel/"
	// PathStyle puts the bucket in the path (MinIO and most self-hosted
	// stores) instead of the host name (AWS default).
	PathStyle bool
}

type Client struct {
	cfg  Config
	base *url.URL
	http *http.Client
	now  func() time.Time // tests
}

func New(cfg Config) (*Client, error) {
	u, err := url.Parse(strings.TrimRight(cfg.Endpoint, "/"))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, errors.New("S3 endpoint must be a full http(s):// address")
	}
	if cfg.Bucket == "" || cfg.AccessKey == "" || cfg.SecretKey == "" {
		return nil, errors.New("S3 needs a bucket, an access key and a secret key")
	}
	if cfg.Region == "" {
		cfg.Region = "us-east-1"
	}
	return &Client{cfg: cfg, base: u, http: &http.Client{Timeout: 2 * time.Minute}, now: time.Now}, nil
}

// Key is where a backup lives in the bucket.
func (c *Client) Key(server, backup string) string {
	return c.cfg.Prefix + server + "/" + backup + ".tar.gz"
}

// target returns the URL and Host for an object.
func (c *Client) target(key string) *url.URL {
	u := *c.base
	if c.cfg.PathStyle {
		u.Path = "/" + c.cfg.Bucket + "/" + key
	} else {
		u.Host = c.cfg.Bucket + "." + c.base.Host
		u.Path = "/" + key
	}
	return &u
}

// ---- Signature Version 4 ----

const algorithm = "AWS4-HMAC-SHA256"

func hmacSHA(key []byte, s string) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(s))
	return h.Sum(nil)
}

func sha(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// escape is RFC 3986 percent-encoding as SigV4 wants it ("/" kept in paths).
func escape(s string, path bool) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch {
		case 'A' <= ch && ch <= 'Z', 'a' <= ch && ch <= 'z', '0' <= ch && ch <= '9', ch == '-', ch == '_', ch == '.', ch == '~':
			b.WriteByte(ch)
		case ch == '/' && path:
			b.WriteByte(ch)
		default:
			fmt.Fprintf(&b, "%%%02X", ch)
		}
	}
	return b.String()
}

func canonicalQuery(q url.Values) string {
	keys := make([]string, 0, len(q))
	for k := range q {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		vs := append([]string(nil), q[k]...)
		sort.Strings(vs)
		for _, v := range vs {
			parts = append(parts, escape(k, false)+"="+escape(v, false))
		}
	}
	return strings.Join(parts, "&")
}

func (c *Client) signingKey(date string) []byte {
	k := hmacSHA([]byte("AWS4"+c.cfg.SecretKey), date)
	k = hmacSHA(k, c.cfg.Region)
	k = hmacSHA(k, "s3")
	return hmacSHA(k, "aws4_request")
}

// signature signs a canonical request made at t.
func (c *Client) signature(t time.Time, method string, u *url.URL, q url.Values, headers map[string]string, payload string) (sig, signed string) {
	names := make([]string, 0, len(headers))
	for k := range headers {
		names = append(names, k)
	}
	sort.Strings(names)
	var ch strings.Builder
	for _, k := range names {
		ch.WriteString(k + ":" + strings.TrimSpace(headers[k]) + "\n")
	}
	signed = strings.Join(names, ";")
	creq := strings.Join([]string{method, escape(u.Path, true), canonicalQuery(q), ch.String(), signed, payload}, "\n")
	date := t.Format("20060102")
	scope := date + "/" + c.cfg.Region + "/s3/aws4_request"
	sts := strings.Join([]string{algorithm, t.Format("20060102T150405Z"), scope, sha([]byte(creq))}, "\n")
	return hex.EncodeToString(hmacSHA(c.signingKey(date), sts)), signed
}

// Presign returns a URL that allows method on key until expires has passed.
// extra adds query parameters (partNumber, uploadId, response-*).
func (c *Client) Presign(method, key string, expires time.Duration, extra url.Values) string {
	t := c.now().UTC()
	u := c.target(key)
	q := url.Values{}
	for k, v := range extra {
		q[k] = v
	}
	q.Set("X-Amz-Algorithm", algorithm)
	q.Set("X-Amz-Credential", c.cfg.AccessKey+"/"+t.Format("20060102")+"/"+c.cfg.Region+"/s3/aws4_request")
	q.Set("X-Amz-Date", t.Format("20060102T150405Z"))
	q.Set("X-Amz-Expires", strconv.Itoa(int(expires.Seconds())))
	q.Set("X-Amz-SignedHeaders", "host")
	sig, _ := c.signature(t, method, u, q, map[string]string{"host": u.Host}, "UNSIGNED-PAYLOAD")
	q.Set("X-Amz-Signature", sig)
	u.RawQuery = canonicalQuery(q)
	return u.String()
}

// do sends a signed request about an object, with a small body.
func (c *Client) do(ctx context.Context, method, key string, q url.Values, body []byte) ([]byte, error) {
	return c.request(ctx, method, c.target(key), q, body)
}

func (c *Client) request(ctx context.Context, method string, u *url.URL, q url.Values, body []byte) ([]byte, error) {
	t := c.now().UTC()
	u.RawQuery = canonicalQuery(q)
	payload := sha(body)
	headers := map[string]string{"host": u.Host, "x-amz-content-sha256": payload, "x-amz-date": t.Format("20060102T150405Z")}
	sig, signed := c.signature(t, method, u, q, headers, payload)
	req, err := http.NewRequestWithContext(ctx, method, u.String(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-amz-content-sha256", payload)
	req.Header.Set("x-amz-date", headers["x-amz-date"])
	req.Header.Set("Authorization", fmt.Sprintf("%s Credential=%s/%s/%s/s3/aws4_request, SignedHeaders=%s, Signature=%s",
		algorithm, c.cfg.AccessKey, t.Format("20060102"), c.cfg.Region, signed, sig))
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode/100 != 2 {
		var e struct {
			Code    string `xml:"Code"`
			Message string `xml:"Message"`
		}
		xml.Unmarshal(out, &e)
		return nil, &Error{Status: resp.StatusCode, Code: e.Code, Message: e.Message}
	}
	return out, nil
}

// Error is a failed S3 request.
type Error struct {
	Status        int
	Code, Message string
}

func (e *Error) Error() string { return fmt.Sprintf("s3: %d %s %s", e.Status, e.Code, e.Message) }

// ---- operations ----

// MinPartSize is the smallest part used. S3 needs at least 5 MiB for every
// part but the last; tests lower it to exercise multipart uploads.
var MinPartSize int64 = 64 << 20

// PartSize is chosen so a backup of any realistic size fits in S3's limit of
// 10 000 parts, while small backups still go up in a single part.
func PartSize(total int64) int64 {
	const maxParts = 10000
	size := MinPartSize
	for total/size >= maxParts {
		size *= 2
	}
	return size
}

// CreateUpload starts a multipart upload and returns its id.
func (c *Client) CreateUpload(ctx context.Context, key string) (string, error) {
	out, err := c.do(ctx, http.MethodPost, key, url.Values{"uploads": {""}}, nil)
	if err != nil {
		return "", err
	}
	var r struct {
		UploadID string `xml:"UploadId"`
	}
	if err := xml.Unmarshal(out, &r); err != nil || r.UploadID == "" {
		return "", errors.New("s3: no upload id in the reply")
	}
	return r.UploadID, nil
}

// PartURLs presigns the PUT for every part of an upload of total bytes.
func (c *Client) PartURLs(key, uploadID string, total int64, expires time.Duration) (partSize int64, urls []string) {
	partSize = PartSize(total)
	n := int((total + partSize - 1) / partSize)
	if n == 0 {
		n = 1 // an empty archive is still one (empty) part
	}
	for i := 1; i <= n; i++ {
		urls = append(urls, c.Presign(http.MethodPut, key, expires, url.Values{"partNumber": {strconv.Itoa(i)}, "uploadId": {uploadID}}))
	}
	return partSize, urls
}

// Part is one uploaded part, as the node reports it.
type Part struct {
	Number int    `json:"number" xml:"PartNumber"`
	ETag   string `json:"etag" xml:"ETag"`
}

// CompleteUpload stitches the parts together into the object.
func (c *Client) CompleteUpload(ctx context.Context, key, uploadID string, parts []Part) error {
	sort.Slice(parts, func(i, j int) bool { return parts[i].Number < parts[j].Number })
	body, _ := xml.Marshal(struct {
		XMLName xml.Name `xml:"CompleteMultipartUpload"`
		Parts   []Part   `xml:"Part"`
	}{Parts: parts})
	out, err := c.do(ctx, http.MethodPost, key, url.Values{"uploadId": {uploadID}}, body)
	if err != nil {
		return err
	}
	// S3 can answer 200 and still report an error in the body.
	if bytes.Contains(out, []byte("<Error>")) {
		return errors.New("s3: completing the upload failed")
	}
	return nil
}

// AbortUpload throws away an unfinished upload and its parts.
func (c *Client) AbortUpload(ctx context.Context, key, uploadID string) error {
	_, err := c.do(ctx, http.MethodDelete, key, url.Values{"uploadId": {uploadID}}, nil)
	return err
}

// Delete removes an object. A missing object is not an error.
func (c *Client) Delete(ctx context.Context, key string) error {
	_, err := c.do(ctx, http.MethodDelete, key, nil, nil)
	var e *Error
	if errors.As(err, &e) && e.Status == http.StatusNotFound {
		return nil
	}
	return err
}

// DownloadURL presigns a GET that the browser saves under name.
func (c *Client) DownloadURL(key, name string, expires time.Duration) string {
	return c.Presign(http.MethodGet, key, expires, url.Values{"response-content-disposition": {`attachment; filename="` + name + `"`}})
}

// CreateBucket makes the bucket (tests and first-time setup). An existing
// bucket that belongs to the caller is fine.
func (c *Client) CreateBucket(ctx context.Context) error {
	u := *c.base
	if c.cfg.PathStyle {
		u.Path = "/" + c.cfg.Bucket
	} else {
		u.Host, u.Path = c.cfg.Bucket+"."+c.base.Host, "/"
	}
	_, err := c.request(ctx, http.MethodPut, &u, nil, nil)
	var e *Error
	if errors.As(err, &e) && (e.Code == "BucketAlreadyOwnedByYou" || e.Code == "BucketAlreadyExists") {
		return nil
	}
	return err
}

// Check writes and deletes a small object, so wrong settings show up when
// the panel starts rather than at the first backup.
func (c *Client) Check(ctx context.Context) error {
	key := c.cfg.Prefix + ".gamepanel-check"
	if _, err := c.do(ctx, http.MethodPut, key, nil, []byte("ok")); err != nil {
		return err
	}
	return c.Delete(ctx, key)
}
