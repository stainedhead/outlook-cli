package graph

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"context"

	"github.com/stainedhead/agent-cli-core/httpx"
	"github.com/stainedhead/agent-cli-core/output"
	"github.com/stainedhead/outlook-cli/internal/domain"
)

// DefaultBaseURL is the Graph v1.0 root.
const DefaultBaseURL = "https://graph.microsoft.com/v1.0"

// DefaultIdempotencyHeader is the internet message header the Sent Items probe
// looks for when Config.IdempotencyHeader is empty.
//
// FR-R6: it is the very header outgoing messages carry (domain.HeaderIdempotencyKey),
// so writer and probe cannot drift apart.
const DefaultIdempotencyHeader = domain.HeaderIdempotencyKey

// maxJSONBytes bounds how much of one JSON response is read.
const maxJSONBytes = 16 << 20

// Config configures a Client.
type Config struct {
	// Refresher authorizes requests (an *auth.Authorizer in production, over
	// authtest.Fake in tests). The adapter never sees a token.
	Refresher httpx.TokenRefresher
	// BaseURL defaults to DefaultBaseURL. Tests point it at an httptest server.
	BaseURL string
	// IdempotencyHeader is the header name FindSentByKey searches.
	IdempotencyHeader string
	// HTTP carries retry tuning (MaxRetries, BaseDelay, MaxWait, Clock, Rand,
	// Trace). Refresher, AllowedHosts and VendorCode are set by New.
	HTTP httpx.Config
	// PageTokenKey supplies the per-install HMAC key (at least 16 bytes) that
	// signs page tokens (FR-R1). Nil, an error or a short key means page
	// tokens cannot be minted or accepted: paging fails closed.
	PageTokenKey func() ([]byte, error)
}

// Client talks to Graph. It implements the three Graph-backed ports. It is
// safe for concurrent use.
type Client struct {
	hc        *http.Client
	base      *url.URL
	baseStr   string
	idemHdr   string
	keyFn     func() ([]byte, error)
	mu        sync.Mutex
	wellKnown map[string]domain.WellKnown // folder id -> alias
}

// New builds a Client. It fails on an unusable BaseURL.
func New(cfg Config) (*Client, error) {
	raw := strings.TrimRight(cfg.BaseURL, "/")
	if raw == "" {
		raw = DefaultBaseURL
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return nil, domain.NewUsage("graph base URL is not a valid http(s) URL")
	}
	hcfg := cfg.HTTP
	hcfg.Refresher = cfg.Refresher
	hcfg.AllowedHosts = []string{u.Host}
	hcfg.VendorCode = vendorCode
	idem := cfg.IdempotencyHeader
	if idem == "" {
		idem = DefaultIdempotencyHeader
	}
	return &Client{hc: httpx.NewClient(hcfg), base: u, baseStr: raw, idemHdr: idem, keyFn: cfg.PageTokenKey}, nil
}

// vendorCode picks a diagnostic value from response headers of a 403.
// ASSUMPTION(unverified against a real tenant): Graph's real error code lives
// in the JSON body, which httpx never offers; x-ms-error-code is a guess and
// request-id is a correlation id, not a code (docs/requested-core-changes.md).
func vendorCode(h http.Header) string {
	for _, k := range []string{"X-Ms-Error-Code", "Request-Id", "Client-Request-Id"} {
		if v := h.Get(k); v != "" {
			return v
		}
	}
	return ""
}

// statusCause is the Cause of domain errors made from an HTTP status; it lets
// the write path decide whether a failure provably was not accepted.
type statusCause struct{ status int }

func (s *statusCause) Error() string { return "HTTP " + strconv.Itoa(s.status) }

// statusError maps a non-2xx status that httpx let through.
func statusError(status int, notFound string) error {
	cause := &statusCause{status}
	return statusErr(status, notFound, cause).WithHTTPStatus(status)
}

func statusErr(status int, notFound string, cause error) *domain.Error {
	switch status {
	case http.StatusNotFound, http.StatusGone:
		return domain.NewNotFound(notFound).WithCause(cause)
	case http.StatusBadRequest:
		return domain.NewValidation("graph rejected the request as invalid (HTTP 400)").WithCause(cause)
	case http.StatusConflict, http.StatusPreconditionFailed:
		return domain.NewConflict("graph reported a conflict (HTTP " + strconv.Itoa(status) + ")").WithCause(cause)
	}
	return domain.NewGeneral("unexpected graph response (HTTP " + strconv.Itoa(status) + ")").WithCause(cause)
}

// httpxStatus is the HTTP status behind a typed httpx error (0 for transport
// failures and anything else), so the audit record can carry it (FR-R13).
func httpxStatus(err error) int {
	var rl *httpx.RateLimitedError
	var fb *httpx.ForbiddenError
	var ae *httpx.AuthError
	switch {
	case errors.As(err, &rl):
		return rl.Status
	case errors.As(err, &fb):
		return http.StatusForbidden
	case errors.As(err, &ae):
		return http.StatusUnauthorized
	}
	return 0
}

// unwrapURLError strips the *url.Error that http.Client adds, so the typed
// httpx and auth errors (and their categories) are what the caller sees.
func unwrapURLError(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) && ue.Err != nil {
		return ue.Err
	}
	return err
}

// do sends one request and returns the response for a 2xx status; the caller
// closes the body. Other statuses and transport failures become errors.
func (c *Client) do(ctx context.Context, method, rawURL string, hdr map[string]string, body any, notFound string) (*http.Response, error) {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, domain.NewGeneral("could not encode request").WithCause(err)
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, rawURL, rdr)
	if err != nil {
		return nil, domain.NewGeneral("could not build request").WithCause(err)
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		err = unwrapURLError(err)
		return nil, domain.WithUpstreamStatus(err, httpxStatus(err))
	}
	if resp.StatusCode/100 == 2 {
		return resp, nil
	}
	_, _ = io.CopyN(io.Discard, resp.Body, 4096)
	_ = resp.Body.Close()
	return nil, statusError(resp.StatusCode, notFound)
}

// getJSON GETs rawURL and decodes the JSON body into out.
func (c *Client) getJSON(ctx context.Context, rawURL string, hdr map[string]string, out any, notFound string) error {
	resp, err := c.do(ctx, http.MethodGet, rawURL, hdr, nil, notFound)
	if err != nil {
		return err
	}
	return decode(resp, out)
}

func decode(resp *http.Response, out any) error {
	defer func() { _ = resp.Body.Close() }()
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxJSONBytes)).Decode(out); err != nil {
		return domain.NewGeneral("malformed response from graph")
	}
	return nil
}

// writeCall sends a mutating request and classifies failures for the ledger.
func (c *Client) writeCall(ctx context.Context, method, rawURL string, body any, out any) error {
	resp, err := c.do(ctx, method, rawURL, nil, body, "message not found")
	if err != nil {
		return classifyWrite(err)
	}
	if out == nil {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		_ = resp.Body.Close()
		return nil
	}
	return decode(resp, out)
}

// classifyWrite wraps domain.NotSent around failures that prove the request
// was not accepted. Anything else (timeout, 5xx, dropped connection) stays
// ambiguous on purpose: the ledger then fails closed.
func classifyWrite(err error) error {
	var sc *statusCause
	if errors.As(err, &sc) {
		if sc.status >= 400 && sc.status < 500 && sc.status != http.StatusRequestTimeout {
			return domain.NotSent(err)
		}
		return err
	}
	switch output.CategoryOf(err) {
	case output.CategoryAuth, output.CategoryForbidden:
		return domain.NotSent(err)
	case output.CategoryRateLimited:
		var rl *httpx.RateLimitedError
		if errors.As(err, &rl) && rl.Status == http.StatusTooManyRequests {
			return domain.NotSent(err)
		}
	}
	return err
}

// meURL builds a URL under /me from already escaped path segments. The only
// root this package ever uses is /me.
func (c *Client) meURL(query string, segs ...string) string {
	var b strings.Builder
	b.WriteString(c.baseStr)
	b.WriteString("/me")
	for _, s := range segs {
		b.WriteString("/")
		b.WriteString(s)
	}
	if query != "" {
		b.WriteString("?")
		b.WriteString(query)
	}
	return b.String()
}

func seg(s string) string { return url.PathEscape(s) }

// esc escapes a query value (spaces as %20, never +).
func esc(s string) string { return strings.ReplaceAll(url.QueryEscape(s), "+", "%20") }

// query joins key/value pairs, skipping empty values. Keys such as $select
// stay literal.
func query(kv ...string) string {
	var parts []string
	for i := 0; i+1 < len(kv); i += 2 {
		if kv[i+1] != "" {
			parts = append(parts, kv[i]+"="+esc(kv[i+1]))
		}
	}
	return strings.Join(parts, "&")
}

// maxIDLen bounds an id used as a path segment. Graph ids are URL-safe base64
// (letters, digits, '-', '_', '=') of a few hundred characters at most.
const maxIDLen = 512

// requireID validates an id (or well-known folder alias) before it becomes a
// path segment (FR-R11). Empty is a validation error as before; anything
// outside the conservative charset, including "." and "..", is a usage error
// and no request is made.
func requireID(kind, id string) error {
	if strings.TrimSpace(id) == "" {
		return domain.NewValidation(fmt.Sprintf("%s id is empty", kind))
	}
	if len(id) > maxIDLen || id == "." || id == ".." {
		return domain.NewUsage(fmt.Sprintf("%s id is not a valid id", kind))
	}
	for i := 0; i < len(id); i++ {
		b := id[i]
		ok := b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b == '-' || b == '_' || b == '='
		if !ok {
			return domain.NewUsage(fmt.Sprintf("%s id is not a valid id", kind))
		}
	}
	return nil
}
