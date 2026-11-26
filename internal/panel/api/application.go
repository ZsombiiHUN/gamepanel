package api

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/ZsombiiHUN/gamepanel/internal/panel/store"
)

// Pterodactyl's application API (/api/application), the part billing
// modules use (WHMCS, Paymenter, Blesta): accounts, servers, nodes,
// locations and eggs, in Pterodactyl's shapes. It takes an administrator's
// API key, never a browser session, and its actions go through the same
// code as GamePanel's own API (serverops.go, accounts.go), so the activity
// log and webhooks see them. docs/api/pterodactyl.md lists what maps how.

// fieldError is a refused input, answered as Pterodactyl's ValidationException
// naming the field.
type fieldError struct{ Field, Rule, Detail string }

func (e *fieldError) Error() string { return e.Detail }

func invalid(field, rule, detail string) error { return &fieldError{field, rule, detail} }

// noNodeError means no node can take a new server. GamePanel's own API
// answers it with the status it carries; the application API with 400, as
// Pterodactyl does (WHMCS then says that no node satisfies the request).
type noNodeError struct{ *huma.ErrorModel }

func (e noNodeError) Unwrap() error { return e.ErrorModel }

func noNode(status int, msg string) error {
	return noNodeError{&huma.ErrorModel{Status: status, Title: http.StatusText(status), Detail: msg}}
}

// appRoute registers an application API handler. Only an administrator's API
// key gets through; errors are answered in Pterodactyl's format.
func (a *App) appRoute(mux *http.ServeMux) func(pattern string, fn func(w http.ResponseWriter, r *http.Request, u store.User) error) {
	return func(pattern string, fn func(w http.ResponseWriter, r *http.Request, u store.User) error) {
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			u, ok := userFrom(r.Context())
			var err error
			switch {
			case !ok:
				err = huma.Error401Unauthorized("Unauthenticated.")
			case !viaKey(r.Context()):
				err = huma.Error403Forbidden("The application API takes an administrator's API key, not a browser session.")
			case !u.IsAdmin:
				err = huma.Error403Forbidden("This action is unauthorized: the application API needs an administrator's API key.")
			default:
				err = fn(w, r, u)
			}
			if err != nil {
				pteroError(w, err)
			}
		})
	}
}

// pteroErrorCodes are Pterodactyl's exception names by status.
var pteroErrorCodes = map[int]string{
	400: "BadRequestHttpException", 401: "AuthenticationException", 403: "AccessDeniedHttpException",
	404: "NotFoundHttpException", 409: "ConflictHttpException", 422: "ValidationException", 429: "TooManyRequestsHttpException",
	502: "HttpException", 503: "HttpException", 504: "HttpException",
}

// pteroError answers in Pterodactyl's error format:
// {"errors":[{"code":"…","status":"404","detail":"…"}]}.
func pteroError(w http.ResponseWriter, err error) {
	status, detail := http.StatusInternalServerError, "Something went wrong on the panel."
	entry := map[string]any{}
	var fe *fieldError
	var nv noNodeError
	var se huma.StatusError
	switch {
	case errors.As(err, &fe):
		status, detail = http.StatusUnprocessableEntity, fe.Detail
		entry["meta"] = map[string]string{"source_field": fe.Field, "rule": fe.Rule}
	case errors.As(err, &nv):
		status, detail = http.StatusBadRequest, nv.Detail
		entry["code"] = "NoViableNodeException"
	case errors.As(err, &se):
		status = se.GetStatus()
		m := &huma.ErrorModel{}
		if errors.As(err, &m) {
			detail = m.Detail
		}
	}
	if _, ok := entry["code"]; !ok {
		code := pteroErrorCodes[status]
		if code == "" {
			code = "HttpException"
		}
		entry["code"] = code
	}
	entry["status"] = strconv.Itoa(status)
	entry["detail"] = detail
	pteroJSON(w, status, map[string]any{"errors": []any{entry}})
}

// readJSON decodes a request body (at most 1 MB) into v.
func readJSON(r *http.Request, v any) error {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20+1))
	if err != nil || len(body) > 1<<20 {
		return huma.Error422UnprocessableEntity("The request body must be JSON of at most 1 MB.")
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return nil
	}
	if err := json.Unmarshal(body, v); err != nil {
		var te *json.UnmarshalTypeError
		if errors.As(err, &te) && te.Field != "" {
			return invalid(te.Field, "type", "The "+te.Field+" field has the wrong type.")
		}
		return huma.Error422UnprocessableEntity("The request body must be JSON.")
	}
	if path := badField(reflect.ValueOf(v), ""); path != "" {
		return invalid(path, "type", "The "+path+" field has the wrong type.")
	}
	return nil
}

// badDecoder is a lenient value that could not make sense of what it got.
// It records that in a bool field named bad instead of failing, because
// encoding/json does not say which field a custom decoder's error came
// from; badField finds it. (The field is read through reflection, which,
// unlike calling isBad, also works inside embedded unexported structs.)
type badDecoder interface{ isBad() bool }

var badDecoderType = reflect.TypeFor[badDecoder]()

// badField returns the JSON path of the first bad value in v, or "".
func badField(v reflect.Value, path string) string {
	if v.Kind() == reflect.Struct && v.Type().Implements(badDecoderType) {
		if v.FieldByName("bad").Bool() {
			return path
		}
		return ""
	}
	switch v.Kind() {
	case reflect.Pointer:
		if !v.IsNil() {
			return badField(v.Elem(), path)
		}
	case reflect.Struct:
		t := v.Type()
		for i := range t.NumField() {
			f := t.Field(i)
			if !f.IsExported() && !f.Anonymous {
				continue
			}
			p := path
			if !f.Anonymous {
				name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
				p = strings.TrimPrefix(path+"."+cmp.Or(name, f.Name), ".")
			}
			if bad := badField(v.Field(i), p); bad != "" {
				return bad
			}
		}
	case reflect.Slice:
		for i := range v.Len() {
			if bad := badField(v.Index(i), path+"."+strconv.Itoa(i)); bad != "" {
				return bad
			}
		}
	}
	return ""
}

// flexInt is an integer that billing modules may send as a number, a
// numeric string, or null, as Laravel's validation accepts.
type flexInt struct {
	V    int64
	Set  bool // present and not null
	Null bool // present as null
	bad  bool
}

func (f flexInt) isBad() bool { return f.bad }

func (f *flexInt) UnmarshalJSON(b []byte) error {
	s := strings.TrimSpace(string(b))
	if s == "null" {
		*f = flexInt{Null: true}
		return nil
	}
	if uq, err := strconv.Unquote(s); err == nil {
		s = strings.TrimSpace(uq)
		if s == "" {
			*f = flexInt{Null: true}
			return nil
		}
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || v != math.Trunc(v) || math.Abs(v) > 1<<53 {
		*f = flexInt{bad: true} // reported by readJSON, with the field's name
		return nil              //nolint:nilerr // see badDecoder
	}
	*f = flexInt{V: int64(v), Set: true}
	return nil
}

// or returns the value, or def when it was left out or null.
func (f flexInt) or(def int64) int64 {
	if f.Set {
		return f.V
	}
	return def
}

// looseBool is a boolean sent as true/false, 1/0 or their strings.
type looseBool struct{ V, Set, bad bool }

func (f looseBool) isBad() bool { return f.bad }

func (f *looseBool) UnmarshalJSON(b []byte) error {
	switch strings.Trim(strings.ToLower(strings.TrimSpace(string(b))), `"`) {
	case "true", "1", "on", "yes":
		*f = looseBool{V: true, Set: true}
	case "false", "0", "off", "no", "", "null":
		*f = looseBool{Set: true}
	default:
		*f = looseBool{bad: true}
	}
	return nil
}

// flexString is a string that may come as a number, a boolean or null (an
// egg variable's value, a port range).
type flexString string

func (f *flexString) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		*f = flexString(s)
		return nil
	}
	switch t := strings.TrimSpace(string(b)); t {
	case "null":
		*f = ""
	case "true":
		*f = "1"
	case "false":
		*f = "0"
	default:
		var n json.Number
		if err := json.Unmarshal(b, &n); err != nil {
			return errors.New("not a string, number or boolean")
		}
		*f = flexString(n.String())
	}
	return nil
}

// optString is a flexString that remembers whether it was sent at all
// (null counts as sent).
type optString struct {
	V   string
	Set bool
}

func (o *optString) UnmarshalJSON(b []byte) error {
	var f flexString
	if err := f.UnmarshalJSON(b); err != nil {
		return err
	}
	*o = optString{V: string(f), Set: true}
	return nil
}

// queryList reads a list parameter however it was sent: name=a,b,
// name[]=a&name[]=b or name[0]=a.
func queryList(q url.Values, name string) []string {
	var out []string
	for k, vs := range q {
		if k != name && !strings.HasPrefix(k, name+"[") {
			continue
		}
		for _, v := range vs {
			for p := range strings.SplitSeq(v, ",") {
				if p = strings.TrimSpace(p); p != "" {
					out = append(out, p)
				}
			}
		}
	}
	return out
}

// includes reports which relationships the caller asked for (?include=).
func includes(r *http.Request) map[string]bool {
	m := map[string]bool{}
	for _, v := range queryList(r.URL.Query(), "include") {
		m[strings.ToLower(v)] = true
	}
	return m
}

// page is the requested page of a list (?page=, ?per_page=).
type page struct{ n, per int }

func pageOf(r *http.Request) page {
	q := r.URL.Query()
	p := page{n: 1, per: 50}
	if v, err := strconv.Atoi(q.Get("page")); err == nil && v > 0 {
		p.n = v
	}
	if v, err := strconv.Atoi(q.Get("per_page")); err == nil && v > 0 {
		p.per = min(v, 500)
	}
	return p
}

// offset is the index of the page's first item.
func (p page) offset() int { return (p.n - 1) * p.per }

// slice returns the page's part of items.
func pageSlice[T any](items []T, p page) []T {
	from := min(p.offset(), len(items))
	return items[from:min(from+p.per, len(items))]
}

// pteroList writes a paginated list of objects of the given kind.
func pteroList(w http.ResponseWriter, kind string, items []any, total int, p page) {
	data := make([]any, 0, len(items))
	for _, it := range items {
		data = append(data, pteroObj{kind, it})
	}
	pages := max((total+p.per-1)/p.per, 1)
	links := map[string]any{}
	pteroJSON(w, http.StatusOK, map[string]any{
		"object": "list", "data": data,
		"meta": map[string]any{"pagination": map[string]any{
			"total": total, "count": len(data), "per_page": p.per, "current_page": p.n, "total_pages": pages, "links": links,
		}},
	})
}

// pteroTime formats a Unix time as Pterodactyl does (ISO 8601, +00:00).
func pteroTime(unix int64) any {
	if unix == 0 {
		return nil
	}
	return time.Unix(unix, 0).UTC().Format("2006-01-02T15:04:05-07:00")
}

// pathID reads a numeric path parameter; anything else is a 404, as in
// Pterodactyl.
func pathID(r *http.Request, name string) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	if err != nil || id <= 0 {
		return 0, huma.Error404NotFound("The requested resource could not be found on the server.")
	}
	return id, nil
}

func notFound() error {
	return huma.Error404NotFound("The requested resource could not be found on the server.")
}

func (a *App) registerApplication(mux *http.ServeMux) {
	a.registerAppUsers(mux)
	a.registerAppServers(mux)
	a.registerAppServerChanges(mux)
	a.registerAppNodes(mux)
	a.registerAppLocations(mux)
	a.registerAppEggs(mux)
}
