package graph

import (
	"encoding/json"
	"testing"
)

func TestFRR4DTOExposesReplyToAndSender(t *testing.T) {
	var m messageDTO
	body := `{"id":"M1","from":{"emailAddress":{"address":"a@x.com"}},
"sender":{"emailAddress":{"name":"S","address":" s@x.com "}},
"replyTo":[{"emailAddress":{"address":"r1@evil.com"}},{"emailAddress":{"address":"r2@evil.com"}}]}`
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		t.Fatal(err)
	}
	rt := m.ReplyToAddresses()
	if len(rt) != 2 || rt[0].Address != "r1@evil.com" || rt[1].Address != "r2@evil.com" {
		t.Fatalf("%+v", rt)
	}
	s, ok := m.SenderAddress()
	if !ok || s.Address != "s@x.com" || s.Name != "S" {
		t.Fatalf("%+v %v", s, ok)
	}
	if _, ok := (messageDTO{}).SenderAddress(); ok {
		t.Fatal("absent sender must report false")
	}
	if (messageDTO{}).ReplyToAddresses() != nil {
		t.Fatal("absent replyTo must be nil")
	}
}

func TestFRR4RawMapsReplyToAndSender(t *testing.T) {
	var m messageDTO
	body := `{"id":"M1","sender":{"emailAddress":{"name":"S","address":"s@x.com"}},
"replyTo":[{"emailAddress":{"address":"r1@evil.com"}}]}`
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		t.Fatal(err)
	}
	r := m.raw()
	if len(r.ReplyTo) != 1 || r.ReplyTo[0].Address != "r1@evil.com" {
		t.Fatalf("ReplyTo = %+v", r.ReplyTo)
	}
	if r.Sender.Address != "s@x.com" {
		t.Fatalf("Sender = %+v", r.Sender)
	}
	if got := (messageDTO{}).raw(); got.ReplyTo != nil || got.Sender.Address != "" {
		t.Fatalf("absent fields must stay empty: %+v", got)
	}
}
