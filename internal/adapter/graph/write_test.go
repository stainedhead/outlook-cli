package graph_test

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stainedhead/agent-cli-core/auth"
	"github.com/stainedhead/agent-cli-core/auth/authtest"
	"github.com/stainedhead/agent-cli-core/httpx"
	"github.com/stainedhead/agent-cli-core/output"
	"github.com/stainedhead/outlook-cli/internal/adapter/graph"
	"github.com/stainedhead/outlook-cli/internal/domain"
)

func msg() domain.OutgoingMessage {
	return domain.OutgoingMessage{
		To:              []domain.Address{{Address: "a@x.com", Name: "A"}},
		Cc:              []domain.Address{{Address: "c@x.com"}},
		Bcc:             []domain.Address{{Address: "b@x.com"}},
		Subject:         "[Agent] hi",
		Body:            "line1\nline2",
		InternetHeaders: []domain.Header{{Name: "X-Agent-Id", Value: "bot"}, {Name: "X-Agent-Run", Value: "r1"}},
	}
}

func TestAssumedSendMailBody(t *testing.T) {
	e := newEnv(t, authtest.Valid, status(202))
	if err := e.c.SendMail(ctx, msg()); err != nil {
		t.Fatal(err)
	}
	s := e.rec.last(t)
	if s.Method != "POST" || s.Path != "/v1.0/me/sendMail" || s.Header.Get("Content-Type") != "application/json" {
		t.Fatalf("%+v", s)
	}
	var got struct {
		Message struct {
			Subject string `json:"subject"`
			Body    struct{ ContentType, Content string }
			To      []struct {
				EmailAddress struct{ Address, Name string }
			} `json:"toRecipients"`
			Cc      []struct{ EmailAddress struct{ Address string } } `json:"ccRecipients"`
			Bcc     []struct{ EmailAddress struct{ Address string } } `json:"bccRecipients"`
			Headers []struct{ Name, Value string }                    `json:"internetMessageHeaders"`
		} `json:"message"`
		Save bool `json:"saveToSentItems"`
	}
	if err := json.Unmarshal([]byte(s.Body), &got); err != nil {
		t.Fatal(err)
	}
	m := got.Message
	if !got.Save || m.Subject != "[Agent] hi" || m.Body.ContentType != "Text" || m.Body.Content != "line1\nline2" ||
		len(m.To) != 1 || m.To[0].EmailAddress.Address != "a@x.com" || m.To[0].EmailAddress.Name != "A" ||
		m.Cc[0].EmailAddress.Address != "c@x.com" || m.Bcc[0].EmailAddress.Address != "b@x.com" ||
		len(m.Headers) != 2 || m.Headers[1].Name != "X-Agent-Run" {
		t.Fatalf("%s", s.Body)
	}
	if strings.Contains(s.Body, `"from"`) {
		t.Fatal("sender must never be set")
	}
}

func TestAssumedCreateDraft(t *testing.T) {
	e := newEnv(t, authtest.Valid, jsonReply(201, `{"id":"D1","subject":"[Agent] hi","isDraft":true}`))
	d, err := e.c.CreateDraft(ctx, msg())
	if err != nil || d.ID != "D1" || !d.IsDraft || d.Subject != "[Agent] hi" {
		t.Fatalf("%+v %v", d, err)
	}
	s := e.rec.last(t)
	if s.Method != "POST" || s.Path != "/v1.0/me/messages" {
		t.Fatalf("%+v", s)
	}
	bad := newEnv(t, authtest.Valid, jsonReply(201, `{}`))
	if _, err := bad.c.CreateDraft(ctx, msg()); output.CategoryOf(err) != output.CategoryGeneral {
		t.Fatal(err)
	}
	bad2 := newEnv(t, authtest.Valid, jsonReply(201, `nope`))
	if _, err := bad2.c.CreateDraft(ctx, msg()); err == nil {
		t.Fatal("malformed draft response must fail")
	}
	bad3 := newEnv(t, authtest.Valid, status(400))
	if _, err := bad3.c.CreateDraft(ctx, msg()); !errors.Is(err, domain.ErrNotSent) {
		t.Fatal(err)
	}
}

func TestAssumedSendDraftDeleteReplyReadMove(t *testing.T) {
	e := newEnv(t, authtest.Valid, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/move") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(201)
			_, _ = io.WriteString(w, `{"id":"NEW1"}`)
			return
		}
		switch r.Method {
		case "DELETE":
			w.WriteHeader(204)
		case "PATCH":
			w.WriteHeader(200)
		default:
			w.WriteHeader(202)
		}
	})
	if err := e.c.SendDraft(ctx, "D1"); err != nil {
		t.Fatal(err)
	}
	if s := e.rec.last(t); s.Method != "POST" || s.Path != "/v1.0/me/messages/D1/send" {
		t.Fatalf("%+v", s)
	}
	if err := e.c.DeleteDraft(ctx, "D1"); err != nil {
		t.Fatal(err)
	}
	if s := e.rec.last(t); s.Method != "DELETE" || s.Path != "/v1.0/me/messages/D1" {
		t.Fatalf("%+v", s)
	}
	if err := e.c.ReplyToSender(ctx, domain.Reply{MessageID: "M1", Body: "thanks"}); err != nil {
		t.Fatal(err)
	}
	if s := e.rec.last(t); s.Method != "POST" || s.Path != "/v1.0/me/messages/M1/reply" || s.Body != `{"comment":"thanks"}` {
		t.Fatalf("%+v", s)
	}
	if err := e.c.SetRead(ctx, "M1", true); err != nil {
		t.Fatal(err)
	}
	if s := e.rec.last(t); s.Method != "PATCH" || s.Path != "/v1.0/me/messages/M1" || s.Body != `{"isRead":true}` {
		t.Fatalf("%+v", s)
	}
	if err := e.c.SetRead(ctx, "M1", false); err != nil || e.rec.last(t).Body != `{"isRead":false}` {
		t.Fatal(err)
	}
	id, err := e.c.MoveMessage(ctx, "M1", "F2")
	if err != nil || id != "NEW1" {
		t.Fatal(id, err)
	}
	if s := e.rec.last(t); s.Method != "POST" || s.Path != "/v1.0/me/messages/M1/move" || s.Body != `{"destinationId":"F2"}` {
		t.Fatalf("%+v", s)
	}
}

func TestMoveWithoutIDInResponseFails(t *testing.T) {
	e := newEnv(t, authtest.Valid, jsonReply(201, `{}`))
	if _, err := e.c.MoveMessage(ctx, "M1", "F2"); output.CategoryOf(err) != output.CategoryGeneral {
		t.Fatal(err)
	}
	e2 := newEnv(t, authtest.Valid, jsonReply(201, `x`))
	if _, err := e2.c.MoveMessage(ctx, "M1", "F2"); err == nil {
		t.Fatal("malformed move response must fail")
	}
}

func TestWriteInputValidationIsNotSentAndLocal(t *testing.T) {
	e := newEnv(t, authtest.Valid, status(200))
	checks := map[string]error{
		"senddraft": e.c.SendDraft(ctx, ""),
		"delete":    e.c.DeleteDraft(ctx, " "),
		"reply":     e.c.ReplyToSender(ctx, domain.Reply{}),
		"setread":   e.c.SetRead(ctx, "", true),
	}
	_, checks["move-msg"] = e.c.MoveMessage(ctx, "", "f")
	_, checks["move-folder"] = e.c.MoveMessage(ctx, "m", "")
	for name, err := range checks {
		if output.CategoryOf(err) != output.CategoryValidation {
			t.Errorf("%s: %v", name, err)
		}
	}
	if !errors.Is(checks["senddraft"], domain.ErrNotSent) || !errors.Is(checks["reply"], domain.ErrNotSent) {
		t.Fatal("local validation of a send must be NotSent")
	}
	if len(e.rec.all()) != 0 {
		t.Fatal("no request may be made")
	}
}

// --- error semantics for writes -------------------------------------------

func TestWriteFailureClassification(t *testing.T) {
	cases := []struct {
		name     string
		h        http.HandlerFunc
		notSent  bool
		category output.Category
		requests int
	}{
		{"400 validation", status(400), true, output.CategoryValidation, 1},
		{"404", status(404), true, output.CategoryNotFound, 1},
		{"409", status(409), true, output.CategoryConflict, 1},
		{"403 forbidden", status(403), true, output.CategoryForbidden, 1},
		{"429 not retried", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(429)
		}, true, output.CategoryRateLimited, 1},
		{"503 ambiguous", status(503), false, output.CategoryRateLimited, 1},
		{"502 ambiguous", status(502), false, output.CategoryRateLimited, 1},
		{"500 ambiguous", status(500), false, output.CategoryGeneral, 1},
		{"408 ambiguous", status(408), false, output.CategoryGeneral, 1},
		{"dropped connection ambiguous", func(w http.ResponseWriter, _ *http.Request) {
			hj, _ := w.(http.Hijacker)
			c, _, _ := hj.Hijack()
			_ = c.Close()
		}, false, output.CategoryGeneral, 1},
	}
	for _, c := range cases {
		e := newEnv(t, authtest.Valid, c.h)
		err := e.c.SendMail(ctx, msg())
		if err == nil {
			t.Fatalf("%s: no error", c.name)
		}
		if errors.Is(err, domain.ErrNotSent) != c.notSent {
			t.Errorf("%s: NotSent=%v want %v (%v)", c.name, !c.notSent, c.notSent, err)
		}
		if got := output.CategoryOf(err); got != c.category && c.name != "dropped connection ambiguous" {
			t.Errorf("%s: category %v want %v (%v)", c.name, got, c.category, err)
		}
		if n := len(e.rec.all()); n != c.requests {
			t.Errorf("%s: POST was sent %d times, want %d (never replay a send)", c.name, n, c.requests)
		}
	}
}

func TestSendMailTimeoutIsAmbiguous(t *testing.T) {
	e := newEnv(t, authtest.Valid, func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() })
	c, cancel := contextWithTimeout(50 * time.Millisecond)
	defer cancel()
	err := e.c.SendMail(c, msg())
	if err == nil || errors.Is(err, domain.ErrNotSent) {
		t.Fatalf("timeout must be ambiguous: %v", err)
	}
}

func TestSendMail401RefreshesOnceAndReplaysSameBody(t *testing.T) {
	// 401 is a rejection before processing, so httpx may replay once after a
	// token refresh; the message is accepted exactly once.
	e := newEnv(t, authtest.UnauthorizedThenSuccess, status(202))
	if err := e.c.SendMail(ctx, msg()); err != nil {
		t.Fatal(err)
	}
	if e.fake.Refreshes() != 1 {
		t.Fatalf("refreshes: %d", e.fake.Refreshes())
	}
	all := e.rec.all()
	if len(all) != 2 || all[0].Body != all[1].Body || all[0].Body == "" {
		t.Fatalf("%d requests; body must be replayed intact", len(all))
	}
}

func TestSecond401IsAuthErrorAndNotSent(t *testing.T) {
	e := newEnv(t, authtest.UnauthorizedTwice, status(202))
	err := e.c.SendMail(ctx, msg())
	var ae *httpx.AuthError
	if !errors.As(err, &ae) || output.CategoryOf(err) != output.CategoryAuth || !errors.Is(err, domain.ErrNotSent) {
		t.Fatalf("%v", err)
	}
	if output.ExitOf(err) != output.ExitCode(3) {
		t.Fatalf("exit %v", output.ExitOf(err))
	}
}

func TestAuthScenariosBeforeSending(t *testing.T) {
	for _, sc := range []authtest.Scenario{authtest.ReauthRequired, authtest.Revoked, authtest.Unreachable} {
		e := newEnv(t, sc, status(202))
		err := e.c.SendMail(ctx, msg())
		if output.CategoryOf(err) != output.CategoryAuth || !errors.Is(err, domain.ErrNotSent) {
			t.Errorf("%v: %v", sc, err)
		}
		if _, err := e.c.Me(ctx); output.CategoryOf(err) != output.CategoryAuth {
			t.Errorf("%v read: %v", sc, err)
		}
		if len(e.rec.all()) != 0 {
			t.Errorf("%v: request reached the server without a token", sc)
		}
	}
	e := newEnv(t, authtest.Unreachable, status(202))
	_, err := e.c.Me(ctx)
	var ue *auth.UnreachableError
	if !errors.As(err, &ue) {
		t.Fatalf("daemon unreachable must surface: %v", err)
	}
}

func TestAssumed403VendorCodeFromHeaders(t *testing.T) {
	e := newEnv(t, authtest.Valid, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Ms-Error-Code", "ErrorAccessDenied")
		w.WriteHeader(403)
	})
	_, err := e.c.Me(ctx)
	var fe *httpx.ForbiddenError
	if !errors.As(err, &fe) || fe.VendorCode != "ErrorAccessDenied" || output.ExitOf(err) != output.ExitCode(4) {
		t.Fatalf("%v", err)
	}
	for hdr, want := range map[string]string{"Request-Id": "rid-1", "Client-Request-Id": "cid-1", "": ""} {
		e := newEnv(t, authtest.Valid, func(w http.ResponseWriter, _ *http.Request) {
			if hdr != "" {
				w.Header().Set(hdr, want)
			}
			w.WriteHeader(403)
		})
		_, err := e.c.Me(ctx)
		if !errors.As(err, &fe) || fe.VendorCode != want {
			t.Errorf("%q: %v", hdr, err)
		}
	}
}

func TestReadRetriesHonourRetryAfter429And503(t *testing.T) {
	for _, code := range []int{429, 503} {
		n := 0
		e := newEnv(t, authtest.Valid, func(w http.ResponseWriter, _ *http.Request) {
			n++
			if n == 1 {
				w.Header().Set("Retry-After", "7")
				w.WriteHeader(code)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"mail":"a@x.com"}`)
		})
		p, err := e.c.Me(ctx)
		if err != nil || p.Mail != "a@x.com" {
			t.Fatalf("%d: %v", code, err)
		}
		if len(e.clk.sleeps) != 1 || e.clk.sleeps[0] != 7*time.Second {
			t.Fatalf("%d: sleeps %v", code, e.clk.sleeps)
		}
	}
}

func TestReadRateLimitPersistsAsExit8(t *testing.T) {
	for _, code := range []int{429, 503} {
		e := newEnv(t, authtest.Valid, status(code))
		_, err := e.c.Me(ctx)
		var rl *httpx.RateLimitedError
		if !errors.As(err, &rl) || rl.Status != code || output.ExitOf(err) != output.ExitCode(8) {
			t.Fatalf("%d: %v", code, err)
		}
		if n := len(e.rec.all()); n != 3 { // 1 + MaxRetries(2)
			t.Fatalf("%d: attempts %d", code, n)
		}
	}
}

func TestRead401RefreshesOnce(t *testing.T) {
	e := newEnv(t, authtest.UnauthorizedThenSuccess, jsonReply(200, `{"mail":"a@x.com"}`))
	p, err := e.c.Me(ctx)
	if err != nil || p.Mail != "a@x.com" || e.fake.Refreshes() != 1 {
		t.Fatalf("%v %v", p, err)
	}
	e2 := newEnv(t, authtest.UnauthorizedTwice, jsonReply(200, `{}`))
	if _, err := e2.c.Me(ctx); output.CategoryOf(err) != output.CategoryAuth {
		t.Fatal(err)
	}
}

func TestExpiredTokenRefreshedViaDaemon(t *testing.T) {
	e := newEnv(t, authtest.ExpiredNeedsRefresh, jsonReply(200, `{"mail":"a@x.com"}`))
	if _, err := e.c.Me(ctx); err != nil {
		t.Fatal(err)
	}
	if e.fake.Refreshes() < 1 {
		t.Fatal("expected a refresh")
	}
}

// --- sent items probe -----------------------------------------------------

func TestAssumedFindSentByKey(t *testing.T) {
	e := newEnv(t, authtest.Valid, jsonReply(200, `{"value":[{"id":"S1"}]}`))
	found, err := e.c.FindSentByKey(ctx, "k'1")
	if err != nil || !found {
		t.Fatal(found, err)
	}
	s := e.rec.last(t)
	q, _ := url.ParseQuery(s.RawQuery)
	if s.Path != "/v1.0/me/mailFolders/sentitems/messages" ||
		q.Get("$filter") != "internetMessageHeaders/any(h:h/name eq 'X-Agent-Idempotency-Key' and h/value eq 'k''1')" {
		t.Fatalf("%+v %v", s, q)
	}
	none := newEnv(t, authtest.Valid, jsonReply(200, `{"value":[]}`))
	if found, err := none.c.FindSentByKey(ctx, "k"); err != nil || found {
		t.Fatal(found, err)
	}
	unsupported := newEnv(t, authtest.Valid, status(400))
	if _, err := unsupported.c.FindSentByKey(ctx, "k"); output.CategoryOf(err) != output.CategoryValidation {
		t.Fatal(err)
	}
	if _, err := none.c.FindSentByKey(ctx, ""); output.CategoryOf(err) != output.CategoryValidation {
		t.Fatal(err)
	}
}

func TestAssumedFindSentByKeyCustomHeader(t *testing.T) {
	e := newEnv(t, authtest.Valid, jsonReply(200, `{"value":[]}`))
	c, err := graph.New(graph.Config{BaseURL: e.srv.URL + "/v1.0", IdempotencyHeader: "X-Agent-Run",
		Refresher: e.token, HTTP: httpx.Config{Clock: e.clk}})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = c.FindSentByKey(ctx, "r1")
	if q, _ := url.ParseQuery(e.rec.last(t).RawQuery); !strings.Contains(q.Get("$filter"), "'X-Agent-Run'") {
		t.Fatal(q)
	}
}

// --- construction & host pinning ------------------------------------------

func TestNewValidatesBaseURL(t *testing.T) {
	for _, u := range []string{"://bad", "ftp://x/y", "/relative"} {
		if _, err := graph.New(graph.Config{BaseURL: u}); output.CategoryOf(err) != output.CategoryUsage {
			t.Errorf("%q: %v", u, err)
		}
	}
	c, err := graph.New(graph.Config{})
	if err != nil || c == nil {
		t.Fatal(err)
	}
}

func TestHostIsPinned(t *testing.T) {
	// A nextLink on another host is never followed: only its cursor is kept
	// and the next request is rebuilt on the configured host (FR-R1).
	e := newEnv(t, authtest.Valid, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("$skiptoken") != "" {
			_, _ = io.WriteString(w, `{"value":[]}`)
			return
		}
		_, _ = io.WriteString(w, `{"value":[],"@odata.nextLink":"https://evil.example/v1.0/me/messages?$skiptoken=zz"}`)
	})
	pg, err := e.c.ListMessages(ctx, domain.MessageQuery{FolderID: "F"})
	if err != nil || pg.NextPageToken == "" {
		t.Fatal(pg, err)
	}
	if _, err := e.c.ListMessages(ctx, domain.MessageQuery{FolderID: "F", PageToken: pg.NextPageToken}); err != nil {
		t.Fatalf("%v", err)
	}
	if s := e.rec.last(t); s.Path != "/v1.0/me/mailFolders/F/messages" {
		t.Fatalf("%+v", s)
	}
	// ListFolders follows nextLink directly: httpx must refuse the foreign host.
	e2 := newEnv(t, authtest.Valid, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"value":[],"@odata.nextLink":"https://evil.example/v1.0/me/mailFolders?x=1"}`)
	})
	_, err = e2.c.ListFolders(ctx)
	var fh *httpx.ForbiddenHostError
	if !errors.As(err, &fh) {
		t.Fatalf("%v", err)
	}
}

func TestAssumedReplyCarriesInternetHeaders(t *testing.T) {
	e := newEnv(t, authtest.Valid, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(202) })
	err := e.c.ReplyToSender(ctx, domain.Reply{MessageID: "M1", Body: "x", InternetHeaders: []domain.Header{{Name: "X-Agent-Run", Value: "r1"}}})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"message":{"internetMessageHeaders":[{"name":"X-Agent-Run","value":"r1"}]},"comment":"x"}`
	if s := e.rec.last(t); s.Body != want {
		t.Fatalf("body %s", s.Body)
	}
}
