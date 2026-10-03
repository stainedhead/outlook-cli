package graph_test

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stainedhead/agent-cli-core/auth/authtest"
	"github.com/stainedhead/agent-cli-core/output"
	"github.com/stainedhead/outlook-cli/internal/domain"
)

var ctx = context.Background()

func TestAssumedMeEndpoint(t *testing.T) {
	e := newEnv(t, authtest.Valid, jsonReply(200, `{"mail":"agent@x.com","userPrincipalName":"agent@x.onmicrosoft.com"}`))
	p, err := e.c.Me(ctx)
	if err != nil || p.Mail != "agent@x.com" || p.UserPrincipalName != "agent@x.onmicrosoft.com" {
		t.Fatalf("%+v %v", p, err)
	}
	s := e.rec.last(t)
	q, _ := url.ParseQuery(s.RawQuery)
	if s.Method != "GET" || s.Path != "/v1.0/me" || q.Get("$select") != "mail,userPrincipalName" {
		t.Fatalf("%+v", s)
	}
	if !strings.HasPrefix(s.Header.Get("Authorization"), "Bearer ") {
		t.Fatal("bearer token not attached")
	}
}

func TestAssumedNeverUsersPathAcrossAllEndpoints(t *testing.T) {
	e := newEnv(t, authtest.Valid, jsonReply(200, `{"value":[],"id":"x"}`))
	_, _ = e.c.Me(ctx)
	_, _ = e.c.ListFolders(ctx)
	_, _ = e.c.ResolveFolder(ctx, "inbox")
	_, _ = e.c.ListMessages(ctx, domain.MessageQuery{FolderID: "f"})
	_, _ = e.c.SearchMessages(ctx, domain.SearchQuery{Text: "a"})
	_, _ = e.c.GetMessage(ctx, "m", true)
	_, _ = e.c.ListAttachments(ctx, "m")
	_, _, _ = e.c.OpenAttachment(ctx, "m", "a")
	_, _ = e.c.ListDrafts(ctx, 5, "")
	_ = e.c.SendMail(ctx, domain.OutgoingMessage{})
	_, _ = e.c.CreateDraft(ctx, domain.OutgoingMessage{})
	_ = e.c.SendDraft(ctx, "d")
	_ = e.c.DeleteDraft(ctx, "d")
	_ = e.c.ReplyToSender(ctx, domain.Reply{MessageID: "m"})
	_ = e.c.SetRead(ctx, "m", true)
	_, _ = e.c.MoveMessage(ctx, "m", "f")
	_, _ = e.c.FindSentByKey(ctx, "k")
	all := e.rec.all()
	if len(all) < 17 {
		t.Fatalf("only %d requests", len(all))
	}
	for _, s := range all {
		if !strings.HasPrefix(s.Path, "/v1.0/me") || strings.Contains(s.Path, "/users") {
			t.Errorf("request outside /me: %s %s", s.Method, s.Path)
		}
	}
}

func TestAssumedListFoldersSetsWellKnownAndPages(t *testing.T) {
	e := newEnv(t, authtest.Valid, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1.0/me/mailFolders":
			if r.URL.Query().Get("page") == "2" {
				_, _ = io.WriteString(w, `{"value":[{"id":"F3","displayName":"Processed","unreadItemCount":0,"totalItemCount":7}]}`)
				return
			}
			_, _ = io.WriteString(w, `{"value":[{"id":"F1","displayName":"Inbox","unreadItemCount":2,"totalItemCount":9},{"id":"F2","displayName":"Sent Items","unreadItemCount":0,"totalItemCount":3}],"@odata.nextLink":"http://`+r.Host+`/v1.0/me/mailFolders?page=2"}`)
		case "/v1.0/me/mailFolders/inbox":
			_, _ = io.WriteString(w, `{"id":"F1"}`)
		case "/v1.0/me/mailFolders/sentitems":
			_, _ = io.WriteString(w, `{"id":"F2"}`)
		default: // other aliases (archive, ...) do not exist
			w.WriteHeader(404)
		}
	})
	fs, err := e.c.ListFolders(ctx)
	if err != nil || len(fs) != 3 {
		t.Fatalf("%v %v", fs, err)
	}
	if fs[0].WellKnown != domain.WellKnownInbox || fs[0].UnreadCount != 2 || fs[0].TotalCount != 9 ||
		fs[1].WellKnown != domain.WellKnownSentItems || fs[2].WellKnown != domain.WellKnownNone || fs[2].Name != "Processed" {
		t.Fatalf("%+v", fs)
	}
	n := len(e.rec.all())
	if _, err := e.c.ListFolders(ctx); err != nil {
		t.Fatal(err)
	}
	if got := len(e.rec.all()) - n; got != 2 { // two pages, alias ids cached
		t.Fatalf("alias lookups were not cached: %d requests", got)
	}
}

func TestAssumedResolveFolder(t *testing.T) {
	e := newEnv(t, authtest.Valid, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1.0/me/mailFolders/sentitems":
			_, _ = io.WriteString(w, `{"id":"S1","displayName":"Sent Items","totalItemCount":3}`)
		case "/v1.0/me/mailFolders":
			_, _ = io.WriteString(w, `{"value":[{"id":"P1","displayName":"Processed"}]}`)
		default:
			w.WriteHeader(404)
		}
	})
	f, err := e.c.ResolveFolder(ctx, "Sent Items")
	if err != nil || f.ID != "S1" || f.WellKnown != domain.WellKnownSentItems || f.TotalCount != 3 {
		t.Fatalf("%+v %v", f, err)
	}
	f, err = e.c.ResolveFolder(ctx, "PROCESSED")
	if err != nil || f.ID != "P1" || f.WellKnown != domain.WellKnownNone {
		t.Fatalf("%+v %v", f, err)
	}
	if _, err = e.c.ResolveFolder(ctx, "nope"); output.CategoryOf(err) != output.CategoryNotFound {
		t.Fatalf("unknown folder: %v", err)
	}
	if _, err = e.c.ResolveFolder(ctx, "inbox"); output.CategoryOf(err) != output.CategoryNotFound {
		t.Fatalf("missing alias folder: %v", err)
	}
	if _, err = e.c.ResolveFolder(ctx, "  "); output.CategoryOf(err) != output.CategoryValidation {
		t.Fatalf("blank: %v", err)
	}
	for in, want := range map[string]string{"junk": "junkemail", "Deleted_Items": "deleteditems", "DRAFTS": "drafts"} {
		_, _ = e.c.ResolveFolder(ctx, in)
		if p := e.rec.last(t).Path; p != "/v1.0/me/mailFolders/"+want {
			t.Errorf("%s -> %s", in, p)
		}
	}
}

const listBody = `{"value":[{"id":"M1","conversationId":"C1","receivedDateTime":"2026-10-02T08:30:00Z",
"from":{"emailAddress":{"name":"Eve","address":" Eve@Ext.com "}},
"toRecipients":[{"emailAddress":{"name":"Agent","address":"agent@x.com"}}],
"subject":"Hi","isRead":false,"isDraft":false,"hasAttachments":true},
{"id":"M2","subject":"nofrom"}],"@odata.nextLink":"%s/v1.0/me/mailFolders/F1/messages?$skiptoken=abc"}`

func TestAssumedListMessagesQueryAndPaging(t *testing.T) {
	e := newEnv(t, authtest.Valid, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		body := strings.Replace(listBody, "%s", "http://"+r.Host, 1)
		_, _ = io.WriteString(w, body)
	})
	since := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	pg, err := e.c.ListMessages(ctx, domain.MessageQuery{FolderID: "F1", UnreadOnly: true, From: "o'brien@x.com", Since: since, Limit: 10})
	if err != nil || len(pg.Items) != 2 || pg.NextPageToken == "" {
		t.Fatalf("%+v %v", pg, err)
	}
	m := pg.Items[0]
	if m.ID != "M1" || m.From.Address != "Eve@Ext.com" || m.From.Name != "Eve" || !m.HasAttachments || m.IsRead ||
		!m.Received.Equal(time.Date(2026, 10, 2, 8, 30, 0, 0, time.UTC)) || len(m.To) != 1 || m.To[0].Address != "agent@x.com" {
		t.Fatalf("%+v", m)
	}
	if pg.Items[1].From.Address != "" {
		t.Fatal("missing from must be empty")
	}
	s := e.rec.last(t)
	q, _ := url.ParseQuery(s.RawQuery)
	if s.Path != "/v1.0/me/mailFolders/F1/messages" || q.Get("$top") != "10" || q.Get("$orderby") != "receivedDateTime desc" ||
		q.Get("$filter") != "receivedDateTime ge 2026-10-01T00:00:00Z and isRead eq false and from/emailAddress/address eq 'o''brien@x.com'" {
		t.Fatalf("%+v %v", s, q)
	}
	if strings.Contains(s.RawQuery, "+") {
		t.Fatal("spaces must be %20")
	}
	// Following the token requests the Graph next link.
	if _, err := e.c.ListMessages(ctx, domain.MessageQuery{PageToken: pg.NextPageToken}); err != nil {
		t.Fatal(err)
	}
	if s := e.rec.last(t); s.Path != "/v1.0/me/mailFolders/F1/messages" || !strings.Contains(s.RawQuery, "skiptoken=abc") {
		t.Fatalf("%+v", s)
	}
}

func TestAssumedListMessagesDefaultsAndValidation(t *testing.T) {
	e := newEnv(t, authtest.Valid, jsonReply(200, `{"value":[]}`))
	pg, err := e.c.ListMessages(ctx, domain.MessageQuery{FolderID: "F1", UnreadOnly: true})
	if err != nil || pg.NextPageToken != "" || len(pg.Items) != 0 {
		t.Fatal(pg, err)
	}
	q, _ := url.ParseQuery(e.rec.last(t).RawQuery)
	if q.Get("$top") != "25" || q.Get("$filter") != "receivedDateTime ge 1970-01-01T00:00:00Z and isRead eq false" {
		t.Fatalf("%v", q)
	}
	_, _ = e.c.ListMessages(ctx, domain.MessageQuery{FolderID: "F1", Limit: 99999})
	if q, _ := url.ParseQuery(e.rec.last(t).RawQuery); q.Get("$top") != "1000" || q.Get("$filter") != "" {
		t.Fatalf("%v", q)
	}
	if _, err := e.c.ListMessages(ctx, domain.MessageQuery{}); output.CategoryOf(err) != output.CategoryValidation {
		t.Fatalf("missing folder: %v", err)
	}
}

func TestInvalidPageTokensAreUsageErrors(t *testing.T) {
	e := newEnv(t, authtest.Valid, jsonReply(200, `{"value":[]}`))
	evil := base64.RawURLEncoding.EncodeToString([]byte("https://evil.example/v1.0/me/messages"))
	otherPath := base64.RawURLEncoding.EncodeToString([]byte(e.srv.URL + "/v1.0/users/x/messages"))
	for _, tok := range []string{"!!!notbase64", evil, otherPath, base64.RawURLEncoding.EncodeToString([]byte("::"))} {
		if _, err := e.c.ListMessages(ctx, domain.MessageQuery{PageToken: tok}); output.CategoryOf(err) != output.CategoryUsage {
			t.Errorf("list %q: %v", tok, err)
		}
		if _, err := e.c.SearchMessages(ctx, domain.SearchQuery{PageToken: tok}); output.CategoryOf(err) != output.CategoryUsage {
			t.Errorf("search %q: %v", tok, err)
		}
		if _, err := e.c.ListDrafts(ctx, 1, tok); output.CategoryOf(err) != output.CategoryUsage {
			t.Errorf("drafts %q: %v", tok, err)
		}
	}
	if n := len(e.rec.all()); n != 0 {
		t.Fatalf("bad tokens must not reach the network: %d", n)
	}
}

func TestAssumedSearchMessages(t *testing.T) {
	e := newEnv(t, authtest.Valid, jsonReply(200, `{"value":[{"id":"M1"}]}`))
	pg, err := e.c.SearchMessages(ctx, domain.SearchQuery{Text: `invoice "Q3" \x`, Limit: 5})
	if err != nil || len(pg.Items) != 1 {
		t.Fatal(pg, err)
	}
	s := e.rec.last(t)
	q, _ := url.ParseQuery(s.RawQuery)
	if s.Path != "/v1.0/me/messages" || q.Get("$search") != `"invoice \"Q3\" \\x"` || q.Get("$top") != "5" || q.Get("$orderby") != "" {
		t.Fatalf("%+v %v", s, q)
	}
	_, _ = e.c.SearchMessages(ctx, domain.SearchQuery{Text: "a", FolderID: "F9"})
	if p := e.rec.last(t).Path; p != "/v1.0/me/mailFolders/F9/messages" {
		t.Fatal(p)
	}
	if _, err := e.c.SearchMessages(ctx, domain.SearchQuery{Text: " "}); output.CategoryOf(err) != output.CategoryValidation {
		t.Fatal(err)
	}
}

func TestAssumedGetMessage(t *testing.T) {
	e := newEnv(t, authtest.Valid, jsonReply(200, `{"id":"M1","subject":"S","body":{"contentType":"text","content":"hello"},
"ccRecipients":[{"emailAddress":{"address":"c@x.com"}}],"internetMessageId":"<a@b>","parentFolderId":"F1",
"internetMessageHeaders":[{"name":"Authentication-Results","value":"spf=pass"}],
"attachments":[{"id":"A1","name":"r.pdf","contentType":"application/pdf","size":12,"isInline":false}]}`))
	m, err := e.c.GetMessage(ctx, "M=1", true)
	if err != nil || m.Body.Format != domain.BodyText || m.Body.Content != "hello" || len(m.Cc) != 1 || m.InternetMessageID != "<a@b>" ||
		m.ParentFolderID != "F1" || len(m.InternetHeaders) != 1 || m.InternetHeaders[0].Name != "Authentication-Results" ||
		len(m.Attachments) != 1 || m.Attachments[0].Size != 12 || m.Attachments[0].Downloadable {
		t.Fatalf("%+v %v", m, err)
	}
	s := e.rec.last(t)
	q, _ := url.ParseQuery(s.RawQuery)
	if s.Path != "/v1.0/me/messages/M=1" || s.Header.Get("Prefer") != `outlook.body-content-type="text"` ||
		!strings.Contains(q.Get("$select"), "internetMessageHeaders") || !strings.HasPrefix(q.Get("$expand"), "attachments(") {
		t.Fatalf("%+v %v", s, q)
	}
	_, _ = e.c.GetMessage(ctx, "M1", false)
	if q, _ := url.ParseQuery(e.rec.last(t).RawQuery); strings.Contains(q.Get("$select"), "internetMessageHeaders") {
		t.Fatal("headers requested although not wanted")
	}
	if _, err := e.c.GetMessage(ctx, "", false); output.CategoryOf(err) != output.CategoryValidation {
		t.Fatal(err)
	}
}

func TestAssumedGetMessageHTMLFallbackAndPathEscaping(t *testing.T) {
	e := newEnv(t, authtest.Valid, jsonReply(200, `{"id":"M1","body":{"contentType":"html","content":"<b>x</b>"}}`))
	m, err := e.c.GetMessage(ctx, "a/b c", false)
	if err != nil || m.Body.Format != domain.BodyHTML {
		t.Fatalf("%+v %v", m, err)
	}
	if p := e.rec.last(t).Path; p != "/v1.0/me/messages/a%2Fb%20c" {
		t.Fatalf("id not escaped: %s", p)
	}
}

func TestAssumedListAndOpenAttachment(t *testing.T) {
	e := newEnv(t, authtest.Valid, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1.0/me/messages/M1/attachments":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"value":[{"id":"A1","name":"r.pdf","contentType":"application/pdf","size":4,"isInline":true}]}`)
		case "/v1.0/me/messages/M1/attachments/A1":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"id":"A1","name":"r.pdf","contentType":"application/pdf","size":4}`)
		case "/v1.0/me/messages/M1/attachments/A1/$value":
			_, _ = io.WriteString(w, "DATA")
		default:
			w.WriteHeader(404)
		}
	})
	as, err := e.c.ListAttachments(ctx, "M1")
	if err != nil || len(as) != 1 || !as[0].IsInline || as[0].Name != "r.pdf" {
		t.Fatalf("%+v %v", as, err)
	}
	a, rc, err := e.c.OpenAttachment(ctx, "M1", "A1")
	if err != nil || a.Size != 4 || a.ID != "A1" {
		t.Fatal(a, err)
	}
	b, _ := io.ReadAll(rc)
	_ = rc.Close()
	if string(b) != "DATA" {
		t.Fatal(string(b))
	}
	if _, _, err := e.c.OpenAttachment(ctx, "M1", "nope"); output.CategoryOf(err) != output.CategoryNotFound {
		t.Fatal(err)
	}
	if _, err := e.c.ListAttachments(ctx, ""); output.CategoryOf(err) != output.CategoryValidation {
		t.Fatal(err)
	}
	if _, _, err := e.c.OpenAttachment(ctx, "", "A"); output.CategoryOf(err) != output.CategoryValidation {
		t.Fatal(err)
	}
	if _, _, err := e.c.OpenAttachment(ctx, "M", ""); output.CategoryOf(err) != output.CategoryValidation {
		t.Fatal(err)
	}
}

func TestAssumedOpenAttachmentContentNotFound(t *testing.T) {
	e := newEnv(t, authtest.Valid, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "$value") {
			w.WriteHeader(404)
			return
		}
		_, _ = io.WriteString(w, `{"id":"A1","size":1}`)
	})
	if _, _, err := e.c.OpenAttachment(ctx, "M1", "A1"); output.CategoryOf(err) != output.CategoryNotFound {
		t.Fatal(err)
	}
}

func TestAssumedListDrafts(t *testing.T) {
	e := newEnv(t, authtest.Valid, jsonReply(200, `{"value":[{"id":"D1","isDraft":true}]}`))
	pg, err := e.c.ListDrafts(ctx, 3, "")
	if err != nil || len(pg.Items) != 1 || !pg.Items[0].IsDraft {
		t.Fatal(pg, err)
	}
	s := e.rec.last(t)
	q, _ := url.ParseQuery(s.RawQuery)
	if s.Path != "/v1.0/me/mailFolders/drafts/messages" || q.Get("$top") != "3" || q.Get("$orderby") != "lastModifiedDateTime desc" {
		t.Fatalf("%+v", s)
	}
}

func TestMalformedAndHostileResponses(t *testing.T) {
	for name, h := range map[string]http.HandlerFunc{
		"badjson": jsonReply(200, `{not json`),
		"empty":   jsonReply(200, ``),
		"badtime": jsonReply(200, `{"value":[{"receivedDateTime":"yesterday"}]}`),
		"redirect": func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "https://evil.example/x", http.StatusFound)
		},
	} {
		e := newEnv(t, authtest.Valid, h)
		_, err := e.c.Me(ctx)
		_, err2 := e.c.ListMessages(ctx, domain.MessageQuery{FolderID: "F"})
		for i, er := range []error{err, err2} {
			if name == "badtime" && i == 0 {
				continue // /me does not parse timestamps
			}
			if er == nil {
				t.Errorf("%s: expected an error", name)
			}
			if name != "redirect" && output.CategoryOf(er) != output.CategoryGeneral {
				t.Errorf("%s: %v", name, er)
			}
			if name == "redirect" && output.CategoryOf(er) != output.CategoryForbidden {
				t.Errorf("redirect must be refused (forbidden host): %v", er)
			}
		}
	}
}

func TestStatusMapping(t *testing.T) {
	cases := map[int]output.Category{
		400: output.CategoryValidation, 404: output.CategoryNotFound, 410: output.CategoryNotFound,
		409: output.CategoryConflict, 412: output.CategoryConflict, 418: output.CategoryGeneral, 500: output.CategoryGeneral,
	}
	for code, want := range cases {
		e := newEnv(t, authtest.Valid, status(code))
		_, err := e.c.Me(ctx)
		if got := output.CategoryOf(err); got != want {
			t.Errorf("%d -> %v, want %v", code, got, want)
		}
	}
}

func TestErrorsNeverContainBodies(t *testing.T) {
	for _, code := range []int{400, 404, 500, 401, 403, 429, 503} {
		code := code
		e := newEnv(t, authtest.Valid, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(code)
			_, _ = io.WriteString(w, "SECRET-BODY-TEXT")
		})
		_, err := e.c.Me(ctx)
		if err == nil || strings.Contains(err.Error(), "SECRET-BODY-TEXT") {
			t.Errorf("%d: %v", code, err)
		}
	}
}

func TestContextCancelled(t *testing.T) {
	e := newEnv(t, authtest.Valid, jsonReply(200, `{}`))
	c, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := e.c.Me(c); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
