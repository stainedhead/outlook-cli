package graph

import (
	"context"
	"strings"

	"github.com/stainedhead/outlook-cli/internal/domain"
)

// FindSentByKey reports whether Sent Items already holds a message carrying
// the idempotency key in the configured header (default
// domain.HeaderIdempotencyKey, the header outgoing messages carry; FR-R6).
//
// ASSUMPTION(unverified against a real tenant): GET
// /me/mailFolders/sentitems/messages with
// $filter=internetMessageHeaders/any(h:h/name eq '<hdr>' and h/value eq '<key>')
// is supported. Graph may reject filtering on internetMessageHeaders (HTTP
// 400); the adapter then returns a validation-category error, and ONLY HTTP
// 400 maps to that category here (the key is checked non-empty first). The use
// case treats exactly that category as "inconclusive" (never as "not sent");
// network, auth and 5xx errors stay hard failures.
func (c *Client) FindSentByKey(ctx context.Context, key string) (bool, error) {
	if strings.TrimSpace(key) == "" {
		return false, domain.NewValidation("idempotency key is empty")
	}
	filter := "internetMessageHeaders/any(h:h/name eq " + odataString(c.idemHdr) + " and h/value eq " + odataString(key) + ")"
	var l messageListDTO
	err := c.getJSON(ctx, c.meURL(query("$filter", filter, "$top", "1", "$select", "id"), "mailFolders", "sentitems", "messages"), nil, &l, "sent items not found")
	if err != nil {
		return false, err
	}
	return len(l.Value) > 0, nil
}
