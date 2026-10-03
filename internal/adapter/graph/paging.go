package graph

import (
	"encoding/base64"
	"net/url"
	"strings"

	"github.com/stainedhead/outlook-cli/internal/domain"
)

// encodeToken turns a Graph @odata.nextLink into the opaque page token.
func encodeToken(nextLink string) string {
	if nextLink == "" {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString([]byte(nextLink))
}

// decodeToken validates a caller supplied token and returns the next link. A
// token that is not one of ours, or that points anywhere but this mailbox on
// the configured Graph host, is a usage error (exit 2).
func (c *Client) decodeToken(tok string) (string, error) {
	bad := domain.NewUsage("invalid page token").WithHint("use the next_page_token from the previous page unchanged")
	raw, err := base64.RawURLEncoding.DecodeString(tok)
	if err != nil {
		return "", bad
	}
	u, err := url.Parse(string(raw))
	if err != nil || u.Scheme != c.base.Scheme || !strings.EqualFold(u.Host, c.base.Host) || u.User != nil {
		return "", bad
	}
	if !strings.HasPrefix(u.Path, c.base.Path+"/me/") {
		return "", bad
	}
	return u.String(), nil
}
