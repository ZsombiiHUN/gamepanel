package daemon

import (
	"context"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ZsombiiHUN/gamepanel/internal/daemon/fsx"
	"github.com/ZsombiiHUN/gamepanel/internal/proto"
)

// pullTimeout bounds a download from the internet into a server.
const pullTimeout = 30 * time.Minute

// fileOp runs one file manager operation.
func (d *Daemon) fileOp(op proto.FileOp) (proto.FileOpResult, error) {
	dir, err := d.dir(op.UUID)
	if err != nil {
		return proto.FileOpResult{}, err
	}
	defer dir.Close()
	if _, err := d.spec(op.UUID); err != nil {
		return proto.FileOpResult{}, err
	}
	defer d.cacheFor(op.UUID).invalidate()
	switch op.Op {
	case proto.OpMove:
		if len(op.Moves) == 0 || len(op.Moves) > 1000 {
			return proto.FileOpResult{}, proto.Invalid("move between 1 and 1000 items at a time")
		}
		for _, m := range op.Moves {
			if err := dir.Move(m.From, m.To); err != nil {
				return proto.FileOpResult{}, fmt.Errorf("%s: %w", path.Base(m.From), err)
			}
		}
		return proto.FileOpResult{}, nil
	case proto.OpCopy:
		to := dir.CopyName(op.Path)
		err := dir.Copy(op.Path, to, func(n int64) error { return d.reserveDisk(op.UUID, n) })
		return proto.FileOpResult{Path: to}, err
	case proto.OpChmod:
		if op.Mode > 0o777 {
			return proto.FileOpResult{}, proto.Invalid("permissions must be between 000 and 777")
		}
		for _, p := range op.Paths {
			if err := dir.Chmod(p, fs.FileMode(op.Mode)); err != nil {
				return proto.FileOpResult{}, fmt.Errorf("%s: %w", path.Base(p), err)
			}
		}
		return proto.FileOpResult{}, nil
	case proto.OpDelete:
		for _, p := range op.Paths {
			if err := dir.Delete(p); err != nil {
				return proto.FileOpResult{}, fmt.Errorf("%s: %w", path.Base(p), err)
			}
		}
		return proto.FileOpResult{}, nil
	case proto.OpCompress:
		if op.Format != "zip" && op.Format != "tar.gz" {
			op.Format = "tar.gz"
		}
		out, err := dir.Compress(op.Root, op.Paths, op.Format, d.writeLimit(op.UUID))
		return proto.FileOpResult{Path: out}, err
	case proto.OpDecompress:
		err := dir.Decompress(op.Path, op.Root, d.writeLimit(op.UUID))
		return proto.FileOpResult{}, err
	}
	return proto.FileOpResult{}, proto.Invalid("unknown file operation")
}

// writeLimit is how many bytes an operation may still write (0 = no limit,
// as fsx expects). A server already at its limit gets 1 byte, which stops
// the first write.
func (d *Daemon) writeLimit(uuid string) int64 {
	room := d.diskHeadroom(uuid)
	switch {
	case room < 0:
		return 0
	case room == 0:
		return 1
	}
	return room
}

// ---- transfers through the panel's relay ----

// relayURL is where a transfer's bytes go to or come from.
func (d *Daemon) relayURL(ticket string) (string, error) {
	u, err := panelHTTP(d.cfg.PanelURL)
	if err != nil {
		return "", err
	}
	u.Path = "/daemon/relay/" + url.PathEscape(ticket)
	return u.String(), nil
}

// panelHTTP turns the panel's websocket address into its HTTP base.
func panelHTTP(ws string) (*url.URL, error) {
	u, err := url.Parse(ws)
	if err != nil {
		return nil, err
	}
	if u.Scheme == "wss" {
		u.Scheme = "https"
	} else {
		u.Scheme = "http"
	}
	u.Path, u.RawQuery = "", ""
	return u, nil
}

func (d *Daemon) relay(ctx context.Context, method, ticket string, body io.Reader, size int64, name string) (*http.Response, error) {
	target, err := d.relayURL(ticket)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+d.cfg.Token)
	if body != nil {
		req.ContentLength = size
		req.Header.Set(proto.HeaderRelaySize, strconv.FormatInt(size, 10))
		req.Header.Set(proto.HeaderRelayName, name)
	}
	resp, err := d.relayClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode/100 != 2 {
		resp.Body.Close()
		return nil, fmt.Errorf("panel relay answered %s", resp.Status)
	}
	return resp, nil
}

// download streams a server file (or a backup) to the panel.
func (d *Daemon) download(ctx context.Context, typ string, t proto.Transfer) (proto.TransferResult, error) {
	var f *os.File
	var size int64
	var name string
	if typ == proto.ReqBackupDownload {
		if !uuidRe.MatchString(t.UUID) || !uuidRe.MatchString(t.BackupID) {
			return proto.TransferResult{}, proto.Invalid("invalid backup id")
		}
		var err error
		f, err = os.Open(d.backupPath(t.UUID, t.BackupID))
		if err != nil {
			return proto.TransferResult{}, proto.NotFound("backup file not found on the node")
		}
		fi, err := f.Stat()
		if err != nil {
			f.Close()
			return proto.TransferResult{}, err
		}
		size, name = fi.Size(), t.BackupID+".tar.gz"
	} else {
		dir, err := d.dir(t.UUID)
		if err != nil {
			return proto.TransferResult{}, err
		}
		defer dir.Close()
		var fi fs.FileInfo
		f, fi, err = dir.Open(t.Path)
		if err != nil {
			return proto.TransferResult{}, err
		}
		size, name = fi.Size(), fi.Name()
		if t.Max > 0 && size > t.Max {
			f.Close()
			return proto.TransferResult{}, fsx.ErrTooLarge
		}
	}
	defer f.Close()
	// The browser is waiting: the panel gives the node a while to connect, then
	// the copy runs as long as bytes keep flowing.
	resp, err := d.relay(ctx, http.MethodPost, t.Ticket, io.LimitReader(f, size), size, name)
	if err != nil {
		return proto.TransferResult{}, err
	}
	resp.Body.Close()
	return proto.TransferResult{Path: t.Path, Size: size}, nil
}

// upload fetches a file the browser is sending and writes it in place.
func (d *Daemon) upload(ctx context.Context, t proto.Transfer) (proto.TransferResult, error) {
	dir, err := d.dir(t.UUID)
	if err != nil {
		return proto.TransferResult{}, err
	}
	defer dir.Close()
	if _, err := d.spec(t.UUID); err != nil {
		return proto.TransferResult{}, err
	}
	if t.Size < 0 {
		return proto.TransferResult{}, proto.Invalid("the upload has no size")
	}
	if err := d.reserveDisk(t.UUID, t.Size); err != nil {
		return proto.TransferResult{}, err
	}
	defer d.cacheFor(t.UUID).invalidate()
	resp, err := d.relay(ctx, http.MethodGet, t.Ticket, nil, 0, "")
	if err != nil {
		return proto.TransferResult{}, err
	}
	defer resp.Body.Close()
	// Exactly the announced size: a body that grows past it is cut off and
	// fails, so the disk reservation always covers what is written.
	n, err := dir.WriteStream(t.Path, io.LimitReader(resp.Body, t.Size+1), t.Size)
	if err == nil && n != t.Size {
		err = proto.Invalid("the upload was interrupted")
	}
	if errors.Is(err, fsx.ErrTooBig) {
		err = proto.Invalid("the upload was larger than announced")
	}
	return proto.TransferResult{Path: t.Path, Size: n}, err
}

// pull downloads a URL into the server. Only http and https, and only to
// public addresses unless the node allows private ones: a game server's
// owner must not be able to make the node fetch from its own network (cloud
// metadata, the Docker API, the panel's admin side).
func (d *Daemon) pull(ctx context.Context, t proto.Transfer) (proto.TransferResult, error) {
	u, err := url.Parse(t.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
		return proto.TransferResult{}, proto.Invalid("use a full http:// or https:// address")
	}
	dir, err := d.dir(t.UUID)
	if err != nil {
		return proto.TransferResult{}, err
	}
	defer dir.Close()
	if _, err := d.spec(t.UUID); err != nil {
		return proto.TransferResult{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, pullTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return proto.TransferResult{}, proto.Invalid("use a full http:// or https:// address")
	}
	req.Header.Set("User-Agent", "GamePanel-daemon/"+Version)
	resp, err := d.pullClient().Do(req)
	if err != nil {
		var blocked *blockedAddr
		if errors.As(err, &blocked) {
			return proto.TransferResult{}, proto.Invalid("that address is on a private network, which this node does not download from")
		}
		return proto.TransferResult{}, proto.Invalid("could not download from that address")
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return proto.TransferResult{}, proto.Invalid("the other server answered " + resp.Status)
	}
	target := t.Path
	if strings.HasSuffix(target, "/") || target == "" {
		target = path.Join(target, pullName(resp, resp.Request.URL)) // after redirects
	}
	limit := d.writeLimit(t.UUID)
	if resp.ContentLength > 0 && limit > 0 && resp.ContentLength > limit {
		return proto.TransferResult{}, ErrDiskFull{d.diskLimitBytes(t.UUID) - limit, d.diskLimitBytes(t.UUID)}
	}
	defer d.cacheFor(t.UUID).invalidate()
	var body io.Reader = resp.Body
	var check func() error
	if t.SHA512 != "" {
		h := sha512.New()
		body = io.TeeReader(resp.Body, h)
		check = func() error {
			if hex.EncodeToString(h.Sum(nil)) != strings.ToLower(t.SHA512) {
				return proto.Invalid("the downloaded file does not match its checksum, so it was not kept")
			}
			return nil
		}
	}
	n, err := dir.WriteStreamCheck(target, body, limit, check)
	if errors.Is(err, fsx.ErrTooBig) {
		err = &proto.Error{Code: proto.CodeDiskFull, Msg: "the download does not fit in the server's disk limit"}
	}
	return proto.TransferResult{Path: "/" + strings.TrimPrefix(target, "/"), Size: n}, err
}

// pullName picks a file name: the server's suggestion, else the URL's last
// segment, else "download". Never a path.
func pullName(resp *http.Response, u *url.URL) string {
	name := ""
	if _, params, err := mime.ParseMediaType(resp.Header.Get("Content-Disposition")); err == nil {
		name = params["filename"]
	}
	if name == "" {
		name = path.Base(u.Path)
	}
	name = strings.TrimSpace(path.Base(strings.ReplaceAll(name, "\\", "/")))
	if name == "" || name == "." || name == "/" || name == ".." || strings.ContainsAny(name, "\x00") {
		name = "download"
	}
	return name
}

type blockedAddr struct{ ip net.IP }

func (b *blockedAddr) Error() string { return "refusing to connect to " + b.ip.String() }

// pullClient checks every address it connects to, after DNS resolution and
// on every redirect, so neither a hostname nor a redirect can lead it inward.
func (d *Daemon) pullClient() *http.Client {
	dialer := &net.Dialer{Timeout: 15 * time.Second}
	if !d.cfg.PullAllowPrivate {
		dialer.Control = func(_, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			ip := net.ParseIP(host)
			if ip == nil || !publicIP(ip) {
				return &blockedAddr{ip}
			}
			return nil
		}
	}
	tr := &http.Transport{DialContext: dialer.DialContext, TLSHandshakeTimeout: 15 * time.Second, ResponseHeaderTimeout: time.Minute, Proxy: nil}
	return &http.Client{Transport: tr, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("too many redirects")
		}
		if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
			return errors.New("redirect to an unsupported scheme")
		}
		return nil
	}}
}

// publicIP reports whether ip is a globally routable unicast address.
func publicIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsInterfaceLocalMulticast() || ip.IsMulticast() {
		return false
	}
	if v4 := ip.To4(); v4 != nil {
		switch {
		case v4[0] == 0, // "this network"
			v4[0] == 100 && v4[1]&0xc0 == 64,         // carrier-grade NAT 100.64/10
			v4[0] == 192 && v4[1] == 0 && v4[2] == 0, // IETF protocol assignments
			v4[0] == 198 && v4[1]&0xfe == 18,         // benchmarking 198.18/15
			v4[0] >= 240:                             // reserved and broadcast
			return false
		}
		return true
	}
	// IPv4-mapped and NAT64 forms of private addresses.
	if len(ip) == net.IPv6len && ip[0] == 0x00 && ip[1] == 0x64 && ip[2] == 0xff && ip[3] == 0x9b {
		return publicIP(net.IP(ip[12:16]))
	}
	return true
}

func (d *Daemon) transfer(ctx context.Context, typ string, raw json.RawMessage) (json.RawMessage, error) {
	var t proto.Transfer
	if err := json.Unmarshal(raw, &t); err != nil {
		return nil, err
	}
	if !uuidRe.MatchString(t.UUID) {
		return nil, proto.Invalid("invalid server id")
	}
	var res proto.TransferResult
	var err error
	switch typ {
	case proto.ReqFilesUpload:
		res, err = d.upload(ctx, t)
	case proto.ReqFilesPull:
		res, err = d.pull(ctx, t)
	default:
		res, err = d.download(ctx, typ, t)
	}
	return proto.Marshal(res), err
}
