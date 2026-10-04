package graph_test

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stainedhead/agent-cli-core/auth/authtest"
	"github.com/stainedhead/agent-cli-core/output"
	"github.com/stainedhead/outlook-cli/internal/domain"
)

// pagedHandler answers every request with one message and a next link that
// carries the given cursor query, on whatever path was requested.
func pagedHandler(cursor string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"value":[{"id":"M1"}],"@odata.nextLink":"http://%s%s?%s"}`, r.Host, r.URL.EscapedPath(), cursor)
	}
}

func TestFRR11IDsRejectedAsPathSegmentsBeforeAnyRequest(t *testing.T) {
	e := newEnv(t, authtest.Valid, jsonReply(200, `{"value":[],"id":"x"}`))
	bad := []string{".", "..", "%2e%2e", "%2E%2E", "a/b", "a b", "a\tb", "a\nb", "a\x00b", "a\\b", "a?b", "a#b", strings.Repeat("A", 513)}
	for _, id := range bad {
		checks := map[string]func() error{
			"get":         func() error { _, err := e.c.GetMessage(ctx, id, false); return err },
			"attachments": func() error { _, err := e.c.ListAttachments(ctx, id); return err },
			"open-msg":    func() error { _, _, err := e.c.OpenAttachment(ctx, id, "a"); return err },
			"open-att":    func() error { _, _, err := e.c.OpenAttachment(ctx, "m", id); return err },
			"list-folder": func() error { _, err := e.c.ListMessages(ctx, domain.MessageQuery{FolderID: id}); return err },
			"search": func() error {
				_, err := e.c.SearchMessages(ctx, domain.SearchQuery{Text: "a", FolderID: id})
				return err
			},
			"delete":      func() error { return e.c.DeleteDraft(ctx, id) },
			"setread":     func() error { return e.c.SetRead(ctx, id, true) },
			"move-msg":    func() error { _, err := e.c.MoveMessage(ctx, id, "f"); return err },
			"move-folder": func() error { _, err := e.c.MoveMessage(ctx, "m", id); return err },
			"senddraft":   func() error { return e.c.SendDraft(ctx, id) },
			"reply":       func() error { return e.c.ReplyToSender(ctx, domain.Reply{MessageID: id}) },
		}
		for name, f := range checks {
			if err := f(); output.CategoryOf(err) != output.CategoryUsage {
				t.Errorf("%s %q: want usage, got %v", name, id, err)
			}
		}
	}
	if n := len(e.rec.all()); n != 0 {
		t.Fatalf("invalid ids must not reach the network: %d requests", n)
	}
}

func TestFRR11RealisticGraphIDsAndAliasesAccepted(t *testing.T) {
	e := newEnv(t, authtest.Valid, jsonReply(200, `{"value":[],"id":"x"}`))
	for _, id := range []string{"AAMkAGI2TG93AAA=", "AAMkAD-_x09=", "inbox", "drafts"} {
		if _, err := e.c.GetMessage(ctx, id, false); err != nil {
			t.Errorf("%q: %v", id, err)
		}
	}
}

func TestFRR1ForgedAndUnsignedTokensRefusedWithoutRequest(t *testing.T) {
	e := newEnv(t, authtest.Valid, jsonReply(200, `{"value":[]}`))
	enc := func(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }
	base := e.srv.URL + "/v1.0"
	toks := []string{
		"", // placeholder replaced below
		"!!!notbase64",
		enc(base + "/me/messages?$select=subject&$top=50"),
		enc(base + "/me/mailFolders/DELETEDID/messages"),
		enc(base + "/me/messages/AAA/attachments"),
		enc(base + "/me/../users/boss@x.com/messages"),
		enc(base + "/me/mailFolders/%2e%2e/messages"),
		enc("https://evil.example/v1.0/me/messages"),
		enc("http://user:pw@" + strings.TrimPrefix(e.srv.URL, "http://") + "/v1.0/me/messages"),
		enc(base + "/me/messages#frag"),
		enc(base + "/users/x/messages"),
		enc("::"),
		"abc.def",
		".",
		"a.b.c",
	}
	for _, tok := range toks[1:] {
		if _, err := e.c.ListMessages(ctx, domain.MessageQuery{FolderID: "F1", PageToken: tok}); output.CategoryOf(err) != output.CategoryUsage {
			t.Errorf("list %q: %v", tok, err)
		}
		if _, err := e.c.SearchMessages(ctx, domain.SearchQuery{Text: "a", FolderID: "F1", PageToken: tok}); output.CategoryOf(err) != output.CategoryUsage {
			t.Errorf("search %q: %v", tok, err)
		}
		if _, err := e.c.ListDrafts(ctx, 1, tok); output.CategoryOf(err) != output.CategoryUsage {
			t.Errorf("drafts %q: %v", tok, err)
		}
	}
	if n := len(e.rec.all()); n != 0 {
		t.Fatalf("refused tokens must send no request: %d", n)
	}
}

func TestFRR1TamperedTokenRefused(t *testing.T) {
	e := newEnv(t, authtest.Valid, pagedHandler("$skiptoken=abc"))
	pg, err := e.c.ListMessages(ctx, domain.MessageQuery{FolderID: "F1", Limit: 5})
	if err != nil || pg.NextPageToken == "" {
		t.Fatal(pg, err)
	}
	hits := len(e.rec.all())
	parts := strings.Split(pg.NextPageToken, ".")
	if len(parts) != 2 {
		t.Fatalf("token shape: %q", pg.NextPageToken)
	}
	// Swap in a payload for another folder, keeping the old MAC.
	evilPayload := base64.RawURLEncoding.EncodeToString([]byte(`{"v":1,"op":"list","f":"DELETED","c":"abc","k":"$skiptoken"}`))
	flipped := parts[0][:len(parts[0])-1] + map[bool]string{true: "B", false: "A"}[parts[0][len(parts[0])-1] == 'A']
	for _, tok := range []string{evilPayload + "." + parts[1], flipped + "." + parts[1], parts[0] + ".", parts[0] + "." + parts[1][:len(parts[1])-2] + "AA"} {
		if _, err := e.c.ListMessages(ctx, domain.MessageQuery{FolderID: "F1", Limit: 5, PageToken: tok}); output.CategoryOf(err) != output.CategoryUsage {
			t.Errorf("%q: %v", tok, err)
		}
	}
	if n := len(e.rec.all()); n != hits {
		t.Fatalf("tampered tokens must send no request: %d -> %d", hits, n)
	}
}

func TestFRR1TokenBoundToFolderOperationAndQuery(t *testing.T) {
	e := newEnv(t, authtest.Valid, pagedHandler("$skiptoken=abc"))
	list, err := e.c.ListMessages(ctx, domain.MessageQuery{FolderID: "A", Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	search, err := e.c.SearchMessages(ctx, domain.SearchQuery{Text: "x", FolderID: "A", Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	drafts, err := e.c.ListDrafts(ctx, 5, "")
	if err != nil {
		t.Fatal(err)
	}
	hits := len(e.rec.all())
	refused := map[string]error{}
	_, refused["list token, folder B"] = e.c.ListMessages(ctx, domain.MessageQuery{FolderID: "B", Limit: 5, PageToken: list.NextPageToken})
	_, refused["list token, search"] = e.c.SearchMessages(ctx, domain.SearchQuery{Text: "x", FolderID: "A", Limit: 5, PageToken: list.NextPageToken})
	_, refused["list token, drafts"] = e.c.ListDrafts(ctx, 5, list.NextPageToken)
	_, refused["list token, other filter"] = e.c.ListMessages(ctx, domain.MessageQuery{FolderID: "A", Limit: 5, UnreadOnly: true, PageToken: list.NextPageToken})
	_, refused["search token, list"] = e.c.ListMessages(ctx, domain.MessageQuery{FolderID: "A", Limit: 5, PageToken: search.NextPageToken})
	_, refused["search token, other text"] = e.c.SearchMessages(ctx, domain.SearchQuery{Text: "y", FolderID: "A", Limit: 5, PageToken: search.NextPageToken})
	_, refused["search token, other folder"] = e.c.SearchMessages(ctx, domain.SearchQuery{Text: "x", FolderID: "B", Limit: 5, PageToken: search.NextPageToken})
	_, refused["drafts token, list drafts folder"] = e.c.ListMessages(ctx, domain.MessageQuery{FolderID: "drafts", Limit: 5, PageToken: drafts.NextPageToken})
	for name, err := range refused {
		if output.CategoryOf(err) != output.CategoryUsage {
			t.Errorf("%s: want usage, got %v", name, err)
		}
	}
	if n := len(e.rec.all()); n != hits {
		t.Fatalf("refused tokens must send no request: %d -> %d", hits, n)
	}
}

func TestFRR1MultiPageWalkListSearchDrafts(t *testing.T) {
	// The next link carries extra keys and a different path; only the cursor
	// may be taken from it.
	e := newEnv(t, authtest.Valid, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("$skiptoken") != "" || r.URL.Query().Get("$skip") != "" {
			_, _ = io.WriteString(w, `{"value":[{"id":"M2"}]}`)
			return
		}
		cur := "$skiptoken=abc%2B1&$filter=evil&$select=body"
		if strings.Contains(r.URL.Path, "drafts") {
			cur = "$skip=10&$top=1"
		}
		_, _ = fmt.Fprintf(w, `{"value":[{"id":"M1"}],"@odata.nextLink":"http://%s/v1.0/me/messages/EVIL?%s"}`, r.Host, cur)
	})
	check := func(name string, first func() (domain.Page[domain.MessageSummary], error), next func(tok string) (domain.Page[domain.MessageSummary], error), path, cursor string) {
		t.Helper()
		pg, err := first()
		if err != nil || pg.NextPageToken == "" {
			t.Fatalf("%s page 1: %+v %v", name, pg, err)
		}
		pg2, err := next(pg.NextPageToken)
		if err != nil || len(pg2.Items) != 1 || pg2.Items[0].ID != "M2" || pg2.NextPageToken != "" {
			t.Fatalf("%s page 2: %+v %v", name, pg2, err)
		}
		s := e.rec.last(t)
		q, _ := url.ParseQuery(s.RawQuery)
		if s.Path != path || q.Get("$filter") == "evil" || q.Get("$select") == "body" || strings.Contains(s.Path, "..") {
			t.Fatalf("%s rebuilt request wrong: %+v", name, s)
		}
		if got := q.Get(cursor[:strings.Index(cursor, "=")]); got != cursor[strings.Index(cursor, "=")+1:] {
			t.Fatalf("%s cursor %q: %+v", name, got, s)
		}
	}
	lq := domain.MessageQuery{FolderID: "F1", Limit: 7}
	check("list", func() (domain.Page[domain.MessageSummary], error) { return e.c.ListMessages(ctx, lq) },
		func(tok string) (domain.Page[domain.MessageSummary], error) {
			q := lq
			q.PageToken = tok
			return e.c.ListMessages(ctx, q)
		}, "/v1.0/me/mailFolders/F1/messages", "$skiptoken=abc+1")
	sq := domain.SearchQuery{Text: "inv", FolderID: "F1", Limit: 7}
	check("search", func() (domain.Page[domain.MessageSummary], error) { return e.c.SearchMessages(ctx, sq) },
		func(tok string) (domain.Page[domain.MessageSummary], error) {
			q := sq
			q.PageToken = tok
			return e.c.SearchMessages(ctx, q)
		}, "/v1.0/me/mailFolders/F1/messages", "$skiptoken=abc+1")
	check("drafts", func() (domain.Page[domain.MessageSummary], error) { return e.c.ListDrafts(ctx, 3, "") },
		func(tok string) (domain.Page[domain.MessageSummary], error) { return e.c.ListDrafts(ctx, 3, tok) },
		"/v1.0/me/mailFolders/drafts/messages", "$skip=10")
	for _, s := range e.rec.all() {
		if strings.Contains(s.Path, "..") {
			t.Errorf("request path contains '..': %s", s.Path)
		}
	}
}

func TestFRR1NextLinkWithoutCursorIsAnError(t *testing.T) {
	e := newEnv(t, authtest.Valid, pagedHandler("$top=5"))
	if _, err := e.c.ListMessages(ctx, domain.MessageQuery{FolderID: "F1"}); err == nil {
		t.Fatal("a next link without a cursor must not yield a token")
	}
	e = newEnv(t, authtest.Valid, pagedHandler("$skip=abc"))
	if _, err := e.c.ListMessages(ctx, domain.MessageQuery{FolderID: "F1"}); err == nil {
		t.Fatal("non numeric $skip must not yield a token")
	}
}

func TestFRR1MissingOrFailingKeyRefusesTokensSafely(t *testing.T) {
	for name, key := range map[string]func() ([]byte, error){
		"nil":     nil,
		"error":   func() ([]byte, error) { return nil, errors.New("no key") },
		"short":   func() ([]byte, error) { return []byte("short"), nil },
		"nil-key": func() ([]byte, error) { return nil, nil },
	} {
		e := newEnvKey(t, authtest.Valid, key, pagedHandler("$skiptoken=abc"))
		// Minting refuses rather than emitting an unauthenticated token.
		if pg, err := e.c.ListMessages(ctx, domain.MessageQuery{FolderID: "F1"}); err == nil || pg.NextPageToken != "" {
			t.Errorf("%s: minting must fail, got %+v %v", name, pg, err)
		}
		hits := len(e.rec.all())
		good := newEnv(t, authtest.Valid, pagedHandler("$skiptoken=abc"))
		pg, err := good.c.ListMessages(ctx, domain.MessageQuery{FolderID: "F1"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := e.c.ListMessages(ctx, domain.MessageQuery{FolderID: "F1", PageToken: pg.NextPageToken}); output.CategoryOf(err) != output.CategoryUsage {
			t.Errorf("%s: token must be refused: %v", name, err)
		}
		if n := len(e.rec.all()); n != hits {
			t.Errorf("%s: no request may follow a refused token", name)
		}
	}
}

func TestFRR6ProbeHeaderIsTheHeaderSendsCarry(t *testing.T) {
	e := newEnv(t, authtest.Valid, jsonReply(200, `{"value":[{"id":"S1"}]}`))
	found, err := e.c.FindSentByKey(ctx, "k1")
	if err != nil || !found {
		t.Fatal(found, err)
	}
	q, _ := url.ParseQuery(e.rec.last(t).RawQuery)
	filter := q.Get("$filter")
	var carried string
	for _, h := range domain.AgentHeaders("a", "r", "k1") {
		if h.Value == "k1" {
			carried = h.Name
		}
	}
	if carried == "" || !strings.Contains(filter, "h/name eq '"+carried+"'") {
		t.Fatalf("probe filter %q does not search the header sends carry (%q)", filter, carried)
	}
}

func TestFRR6ProbeHTTP400IsAValidationErrorOthersAreNot(t *testing.T) {
	e := newEnv(t, authtest.Valid, status(400))
	if _, err := e.c.FindSentByKey(ctx, "k"); output.CategoryOf(err) != output.CategoryValidation {
		t.Fatalf("HTTP 400 must surface as a validation error (inconclusive for the use case): %v", err)
	}
	e = newEnv(t, authtest.Valid, status(503))
	if _, err := e.c.FindSentByKey(ctx, "k"); err == nil || output.CategoryOf(err) == output.CategoryValidation {
		t.Fatalf("5xx must stay a hard error: %v", err)
	}
}

func TestFRR4GetMessageSelectsReplyToAndSender(t *testing.T) {
	e := newEnv(t, authtest.Valid, jsonReply(200, `{"id":"M1"}`))
	if _, err := e.c.GetMessage(ctx, "M1", false); err != nil {
		t.Fatal(err)
	}
	q, _ := url.ParseQuery(e.rec.last(t).RawQuery)
	sel := "," + q.Get("$select") + ","
	if !strings.Contains(sel, ",replyTo,") || !strings.Contains(sel, ",sender,") {
		t.Fatalf("$select must include replyTo and sender: %s", q.Get("$select"))
	}
}
