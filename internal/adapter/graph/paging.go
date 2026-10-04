package graph

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"strings"
	"unicode"

	"github.com/stainedhead/outlook-cli/internal/domain"
)

// FR-R1: a page token is an authenticated reference, never a URL. It carries
// only the paging cursor Graph handed back, bound to the operation, the
// folder and the exact request the first page answered, and signed with a
// per-install key. The next request is rebuilt from the caller's own query;
// nothing but the cursor comes from the token, so a token can never address
// another folder, collection or user.

const (
	tokenVersion   = 1
	tokenDomain    = "outlook-page-token-v1\x00"
	minKeyBytes    = 16
	maxCursorBytes = 4096

	opList   = "list"
	opSearch = "search"
	opDrafts = "drafts"

	cursorSkipToken = "$skiptoken"
	cursorSkip      = "$skip"
)

// pageSpec describes one paged collection request: which operation, which
// folder, the collection path and the query without any cursor.
type pageSpec struct {
	op     string
	folder string
	segs   []string
	qs     string
}

func (s pageSpec) fingerprint() string {
	h := sha256.Sum256([]byte(s.op + "\x00" + s.folder + "\x00" + s.qs))
	return hex.EncodeToString(h[:])
}

type tokenPayload struct {
	V  int    `json:"v"`
	Op string `json:"op"`
	F  string `json:"f"`
	H  string `json:"h"`
	K  string `json:"k"`
	C  string `json:"c"`
}

func errBadToken() error {
	return domain.NewUsage("invalid page token").WithHint("use the next_page_token from the previous page unchanged, with the same command and flags")
}

// pageKey returns the HMAC key, or an error when none is usable. A missing
// provider, a provider error and a short key all refuse (nothing is ever
// signed with a guessable key).
func (c *Client) pageKey() ([]byte, error) {
	if c.keyFn == nil {
		return nil, errNoKey()
	}
	k, err := c.keyFn()
	if err != nil || len(k) < minKeyBytes {
		return nil, errNoKey()
	}
	return k, nil
}

func errNoKey() error {
	return domain.NewGeneral("page tokens are unavailable: no page token key is configured")
}

func sign(key []byte, payloadB64 string) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(tokenDomain))
	m.Write([]byte(payloadB64))
	return m.Sum(nil)
}

// mintToken turns the next link Graph returned for spec into a signed token,
// keeping only its paging cursor. An empty next link means the last page.
func (c *Client) mintToken(spec pageSpec, nextLink string) (string, error) {
	if nextLink == "" {
		return "", nil
	}
	u, err := url.Parse(nextLink)
	if err != nil {
		return "", domain.NewGeneral("graph returned an unusable next page link")
	}
	name, val := cursorOf(u.Query())
	if name == "" {
		return "", domain.NewGeneral("graph returned a next page link without a paging cursor")
	}
	key, err := c.pageKey()
	if err != nil {
		return "", err
	}
	b, err := json.Marshal(tokenPayload{V: tokenVersion, Op: spec.op, F: spec.folder, H: spec.fingerprint(), K: name, C: val})
	if err != nil {
		return "", domain.NewGeneral("could not encode page token")
	}
	p := base64.RawURLEncoding.EncodeToString(b)
	return p + "." + base64.RawURLEncoding.EncodeToString(sign(key, p)), nil
}

// cursorOf extracts a safe paging cursor from a next link query.
func cursorOf(q url.Values) (name, val string) {
	if v := q.Get(cursorSkipToken); v != "" {
		name, val = cursorSkipToken, v
	} else if v := q.Get(cursorSkip); v != "" {
		name, val = cursorSkip, v
		for _, r := range v {
			if r < '0' || r > '9' {
				return "", ""
			}
		}
	} else {
		return "", ""
	}
	if len(val) > maxCursorBytes || strings.IndexFunc(val, unicode.IsControl) >= 0 {
		return "", ""
	}
	return name, val
}

// openToken verifies tok against spec and returns the cursor query pair. Every
// failure is the same usage error; no request has been made at this point.
func (c *Client) openToken(spec pageSpec, tok string) (name, val string, err error) {
	key, kerr := c.pageKey()
	if kerr != nil {
		return "", "", errBadToken()
	}
	parts := strings.Split(tok, ".")
	if len(parts) != 2 {
		return "", "", errBadToken()
	}
	mac, merr := base64.RawURLEncoding.DecodeString(parts[1])
	if merr != nil || !hmac.Equal(mac, sign(key, parts[0])) {
		return "", "", errBadToken()
	}
	raw, derr := base64.RawURLEncoding.DecodeString(parts[0])
	if derr != nil {
		return "", "", errBadToken()
	}
	var p tokenPayload
	if json.Unmarshal(raw, &p) != nil || p.V != tokenVersion || p.Op != spec.op || p.F != spec.folder ||
		!hmac.Equal([]byte(p.H), []byte(spec.fingerprint())) {
		return "", "", errBadToken()
	}
	if (p.K != cursorSkipToken && p.K != cursorSkip) || p.C == "" || len(p.C) > maxCursorBytes {
		return "", "", errBadToken()
	}
	return p.K, p.C, nil
}
