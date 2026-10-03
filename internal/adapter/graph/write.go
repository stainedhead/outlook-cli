package graph

import (
	"context"
	"net/http"

	"github.com/stainedhead/outlook-cli/internal/domain"
)

// None of the POSTs here is passed through httpx.MarkSafeToRetry: a replay
// could send twice, so a failed attempt is surfaced instead of retried. The
// ledger is the idempotency mechanism.

type sendMailDTO struct {
	Message         outgoingDTO `json:"message"`
	SaveToSentItems bool        `json:"saveToSentItems"`
}

type replyDTO struct {
	Comment string `json:"comment"`
}

type moveDTO struct {
	DestinationID string `json:"destinationId"`
}

type readDTO struct {
	IsRead bool `json:"isRead"`
}

// SendMail sends a message from the signed-in mailbox.
//
// ASSUMPTION(unverified against a real tenant): POST /me/sendMail with
// {message, saveToSentItems:true} answers 202 with no body; custom
// internetMessageHeaders must start with "x-" and Graph caps how many are
// accepted.
func (c *Client) SendMail(ctx context.Context, m domain.OutgoingMessage) error {
	return c.writeCall(ctx, http.MethodPost, c.meURL("", "sendMail"), sendMailDTO{Message: outgoing(m), SaveToSentItems: true}, nil)
}

// CreateDraft saves a draft.
//
// ASSUMPTION(unverified against a real tenant): POST /me/messages with a
// message resource answers 201 with the saved message including its id.
func (c *Client) CreateDraft(ctx context.Context, m domain.OutgoingMessage) (domain.Draft, error) {
	var out messageDTO
	if err := c.writeCall(ctx, http.MethodPost, c.meURL("", "messages"), outgoing(m), &out); err != nil {
		return domain.Draft{}, err
	}
	if out.ID == "" {
		return domain.Draft{}, domain.NewGeneral("graph returned a draft without an id")
	}
	return domain.Draft{ID: out.ID, MessageSummary: out.summary()}, nil
}

// SendDraft sends a saved draft.
//
// ASSUMPTION(unverified against a real tenant): POST /me/messages/{id}/send
// answers 202 with no body.
func (c *Client) SendDraft(ctx context.Context, draftID string) error {
	if err := requireID("draft", draftID); err != nil {
		return domain.NotSent(err)
	}
	return c.writeCall(ctx, http.MethodPost, c.meURL("", "messages", seg(draftID), "send"), nil, nil)
}

// DeleteDraft deletes one draft.
//
// ASSUMPTION(unverified against a real tenant): DELETE /me/messages/{id}
// answers 204.
func (c *Client) DeleteDraft(ctx context.Context, draftID string) error {
	if err := requireID("draft", draftID); err != nil {
		return err
	}
	return c.writeCall(ctx, http.MethodDelete, c.meURL("", "messages", seg(draftID)), nil, nil)
}

// ReplyToSender replies to the sender only.
//
// ASSUMPTION(unverified against a real tenant): POST /me/messages/{id}/reply
// with {"comment": body} answers 202; the comment is inserted above the
// quoted original (and may be treated as HTML).
func (c *Client) ReplyToSender(ctx context.Context, r domain.Reply) error {
	if err := requireID("message", r.MessageID); err != nil {
		return domain.NotSent(err)
	}
	return c.writeCall(ctx, http.MethodPost, c.meURL("", "messages", seg(r.MessageID), "reply"), replyDTO{Comment: r.Body}, nil)
}

// SetRead sets the isRead flag.
//
// ASSUMPTION(unverified against a real tenant): PATCH /me/messages/{id} with
// {"isRead": bool} answers 200.
func (c *Client) SetRead(ctx context.Context, messageID string, read bool) error {
	if err := requireID("message", messageID); err != nil {
		return err
	}
	return c.writeCall(ctx, http.MethodPatch, c.meURL("", "messages", seg(messageID)), readDTO{IsRead: read}, nil)
}

// MoveMessage moves a message to a folder and returns the new id.
//
// ASSUMPTION(unverified against a real tenant): POST /me/messages/{id}/move
// with {"destinationId": folderId} answers 201 with the moved message; the id
// changes because the message gets a new parent.
func (c *Client) MoveMessage(ctx context.Context, messageID, folderID string) (string, error) {
	if err := requireID("message", messageID); err != nil {
		return "", err
	}
	if err := requireID("folder", folderID); err != nil {
		return "", err
	}
	var out messageDTO
	if err := c.writeCall(ctx, http.MethodPost, c.meURL("", "messages", seg(messageID), "move"), moveDTO{DestinationID: folderID}, &out); err != nil {
		return "", err
	}
	if out.ID == "" {
		return "", domain.NewGeneral("graph returned a moved message without an id")
	}
	return out.ID, nil
}
