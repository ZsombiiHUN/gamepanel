package api

import (
	"bufio"
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"net"
	"net/http"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ZsombiiHUN/gamepanel/internal/version"
)

// Operations endpoints, outside /api:
//
//	GET /healthz  the process serves requests (liveness)
//	GET /readyz   ... and its database answers (readiness)
//	GET /metrics  Prometheus text format; needs MetricsToken as a bearer
//	              token, and is not served at all without one
//
// None of them say anything about accounts or servers beyond counts.

func (a *App) registerOps(mux *http.ServeMux) {
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		if err := a.Store.Ping(ctx); err != nil {
			a.Log.Warn("readiness check: the database does not answer", "err", err)
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("database unavailable\n"))
			return
		}
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("GET /metrics", a.metricsHandler)
}

var started = time.Now()

func (a *App) metricsHandler(w http.ResponseWriter, r *http.Request) {
	tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if a.MetricsToken == "" {
		http.NotFound(w, r)
		return
	}
	if !ok || subtle.ConstantTimeCompare([]byte(tok), []byte(a.MetricsToken)) != 1 {
		w.Header().Set("WWW-Authenticate", `Bearer realm="metrics"`)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	var b strings.Builder
	m := promWriter{&b}
	m.gauge("gamepanel_build_info", "The panel's version.", 1, "version", version.Version)
	m.gauge("gamepanel_start_time_seconds", "When the panel started, in Unix time.", float64(started.Unix()))
	if t, err := a.Store.Totals(ctx); err == nil {
		m.gauge("gamepanel_users", "Accounts.", float64(t.Users))
		m.gauge("gamepanel_nodes", "Nodes.", float64(t.Nodes))
		m.gauge("gamepanel_servers", "Game servers.", float64(t.Servers))
		m.gauge("gamepanel_servers_suspended", "Suspended game servers.", float64(t.SuspendedServers))
		m.header("gamepanel_webhook_deliveries", "Webhook deliveries by status (pending: waiting for a first try or a retry).", "gauge")
		m.sample("gamepanel_webhook_deliveries", float64(t.PendingDeliveries), "status", "pending")
		m.sample("gamepanel_webhook_deliveries", float64(t.FailedDeliveries), "status", "failed")
		m.gauge("gamepanel_database_up", "Whether the database answered.", 1)
	} else {
		a.Log.Warn("metrics: could not count", "err", err)
		m.gauge("gamepanel_database_up", "Whether the database answered.", 0)
	}
	m.gauge("gamepanel_nodes_connected", "Nodes whose daemon is connected.", float64(a.Hub.Connected()))
	httpStats.write(m)
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	m.gauge("go_goroutines", "Goroutines.", float64(runtime.NumGoroutine()))
	m.gauge("go_memstats_heap_alloc_bytes", "Bytes of allocated heap objects.", float64(ms.HeapAlloc))
	m.gauge("go_memstats_sys_bytes", "Bytes obtained from the system.", float64(ms.Sys))
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	_, _ = w.Write([]byte(b.String()))
}

type promWriter struct{ b *strings.Builder }

func (p promWriter) header(name, help, typ string) {
	fmt.Fprintf(p.b, "# HELP %s %s\n# TYPE %s %s\n", name, help, name, typ)
}

// sample writes one line; labels are name, value pairs.
func (p promWriter) sample(name string, v float64, labels ...string) {
	p.b.WriteString(name)
	if len(labels) > 0 {
		p.b.WriteByte('{')
		for i := 0; i+1 < len(labels); i += 2 {
			if i > 0 {
				p.b.WriteByte(',')
			}
			fmt.Fprintf(p.b, "%s=%q", labels[i], labels[i+1])
		}
		p.b.WriteByte('}')
	}
	p.b.WriteByte(' ')
	p.b.WriteString(strconv.FormatFloat(v, 'g', -1, 64))
	p.b.WriteByte('\n')
}

func (p promWriter) gauge(name, help string, v float64, labels ...string) {
	p.header(name, help, "gauge")
	p.sample(name, v, labels...)
}

// ---- HTTP request metrics ----

// Request durations are kept per area (api, daemon, web, ops) and status
// class, never per path: paths hold ids, which would make every server a
// new time series.
var durationBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}

type reqKey struct{ area, method, class string }

type reqSeries struct {
	count   atomic.Uint64
	sum     atomic.Uint64 // microseconds
	buckets []atomic.Uint64
}

type requestStats struct{ m sync.Map } // reqKey -> *reqSeries

var httpStats requestStats

func (s *requestStats) observe(area, method string, status int, d time.Duration) {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions:
	default:
		method = "other"
	}
	k := reqKey{area, method, strconv.Itoa(status/100) + "xx"}
	v, ok := s.m.Load(k)
	if !ok {
		v, _ = s.m.LoadOrStore(k, &reqSeries{buckets: make([]atomic.Uint64, len(durationBuckets))})
	}
	r, ok := v.(*reqSeries)
	if !ok {
		return
	}
	r.count.Add(1)
	r.sum.Add(uint64(max(d.Microseconds(), 0)))
	sec := d.Seconds()
	for i, le := range durationBuckets {
		if sec <= le {
			r.buckets[i].Add(1)
		}
	}
}

func (s *requestStats) write(m promWriter) {
	type entry struct {
		k reqKey
		r *reqSeries
	}
	var all []entry
	s.m.Range(func(k, v any) bool {
		key, ok1 := k.(reqKey)
		r, ok2 := v.(*reqSeries)
		if ok1 && ok2 {
			all = append(all, entry{key, r})
		}
		return true
	})
	sort.Slice(all, func(i, j int) bool {
		a, b := all[i].k, all[j].k
		return a.area+a.method+a.class < b.area+b.method+b.class
	})
	const name = "gamepanel_http_request_duration_seconds"
	m.header(name, "HTTP requests by area, method and status class, and how long they took (websockets and streams count until the response starts).", "histogram")
	for _, e := range all {
		l := []string{"area", e.k.area, "method", e.k.method, "code", e.k.class}
		for i, le := range durationBuckets {
			m.sample(name+"_bucket", float64(e.r.buckets[i].Load()), append(l, "le", strconv.FormatFloat(le, 'g', -1, 64))...)
		}
		n := float64(e.r.count.Load())
		m.sample(name+"_bucket", n, append(l, "le", "+Inf")...)
		m.sample(name+"_sum", float64(e.r.sum.Load())/1e6, l...)
		m.sample(name+"_count", n, l...)
	}
}

// area groups a path for metrics.
func area(path string) string {
	switch {
	case strings.HasPrefix(path, "/api/"):
		return "api"
	case strings.HasPrefix(path, "/daemon/"):
		return "daemon"
	case path == "/healthz" || path == "/readyz" || path == "/metrics":
		return "ops"
	}
	return "web"
}

// statusRecorder remembers the status and when the response started. It
// passes Flush and Hijack through, which streams and websockets need.
type statusRecorder struct {
	http.ResponseWriter
	status  int
	started time.Time // when the header was written
}

func (s *statusRecorder) WriteHeader(code int) {
	if s.status == 0 {
		s.status, s.started = code, time.Now()
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status, s.started = http.StatusOK, time.Now()
	}
	return s.ResponseWriter.Write(b)
}

func (s *statusRecorder) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (s *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := s.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("hijacking is not supported")
	}
	if s.status == 0 {
		s.status, s.started = http.StatusSwitchingProtocols, time.Now()
	}
	return h.Hijack()
}

func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// instrument records each request in httpStats.
func instrument(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)
		end := rec.started
		if rec.status == 0 {
			rec.status, end = http.StatusOK, time.Now()
		}
		httpStats.observe(area(r.URL.Path), r.Method, rec.status, end.Sub(start))
	})
}
