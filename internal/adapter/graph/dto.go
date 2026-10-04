package graph

import (
	"strings"
	"time"

	"github.com/stainedhead/outlook-cli/internal/domain"
)

// Graph DTOs live only in this package.

type emailAddressDTO struct {
	Name    string `json:"name,omitempty"`
	Address string `json:"address"`
}

type recipientDTO struct {
	EmailAddress emailAddressDTO `json:"emailAddress"`
}

type bodyDTO struct {
	ContentType string `json:"contentType"`
	Content     string `json:"content"`
}

type headerDTO struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type attachmentDTO struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	ContentType string `json:"contentType"`
	Size        int64  `json:"size"`
	IsInline    bool   `json:"isInline"`
}

type messageDTO struct {
	ID                     string          `json:"id"`
	ConversationID         string          `json:"conversationId"`
	ReceivedDateTime       time.Time       `json:"receivedDateTime"`
	From                   *recipientDTO   `json:"from"`
	ToRecipients           []recipientDTO  `json:"toRecipients"`
	CcRecipients           []recipientDTO  `json:"ccRecipients"`
	BccRecipients          []recipientDTO  `json:"bccRecipients"`
	ReplyTo                []recipientDTO  `json:"replyTo"`
	Sender                 *recipientDTO   `json:"sender"`
	Subject                string          `json:"subject"`
	IsRead                 bool            `json:"isRead"`
	IsDraft                bool            `json:"isDraft"`
	HasAttachments         bool            `json:"hasAttachments"`
	Body                   *bodyDTO        `json:"body"`
	InternetMessageID      string          `json:"internetMessageId"`
	ParentFolderID         string          `json:"parentFolderId"`
	InternetMessageHeaders []headerDTO     `json:"internetMessageHeaders"`
	Attachments            []attachmentDTO `json:"attachments"`
}

type messageListDTO struct {
	Value    []messageDTO `json:"value"`
	NextLink string       `json:"@odata.nextLink"`
}

type folderDTO struct {
	ID              string `json:"id"`
	DisplayName     string `json:"displayName"`
	UnreadItemCount int    `json:"unreadItemCount"`
	TotalItemCount  int    `json:"totalItemCount"`
}

type folderListDTO struct {
	Value    []folderDTO `json:"value"`
	NextLink string      `json:"@odata.nextLink"`
}

type attachmentListDTO struct {
	Value []attachmentDTO `json:"value"`
}

type profileDTO struct {
	Mail              string `json:"mail"`
	UserPrincipalName string `json:"userPrincipalName"`
}

func toAddress(r recipientDTO) domain.Address {
	return domain.Address{Address: strings.TrimSpace(r.EmailAddress.Address), Name: r.EmailAddress.Name}
}

func toAddresses(rs []recipientDTO) []domain.Address {
	if len(rs) == 0 {
		return nil
	}
	out := make([]domain.Address, 0, len(rs))
	for _, r := range rs {
		out = append(out, toAddress(r))
	}
	return out
}

func (m messageDTO) summary() domain.MessageSummary {
	s := domain.MessageSummary{
		ID:             m.ID,
		ConversationID: m.ConversationID,
		Received:       m.ReceivedDateTime,
		To:             toAddresses(m.ToRecipients),
		Subject:        m.Subject,
		IsRead:         m.IsRead,
		IsDraft:        m.IsDraft,
		HasAttachments: m.HasAttachments,
	}
	if m.From != nil {
		s.From = toAddress(*m.From)
	}
	return s
}

// ReplyToAddresses returns the message's Reply-To addresses (FR-R4). The use
// case evaluates policy against them because Graph's reply action may address
// the reply there (unverified against a real tenant).
func (m messageDTO) ReplyToAddresses() []domain.Address { return toAddresses(m.ReplyTo) }

// SenderAddress returns the Sender header address, if the message has one.
func (m messageDTO) SenderAddress() (domain.Address, bool) {
	if m.Sender == nil {
		return domain.Address{}, false
	}
	return toAddress(*m.Sender), true
}

func (a attachmentDTO) toDomain() domain.Attachment {
	return domain.Attachment{ID: a.ID, Name: a.Name, ContentType: a.ContentType, Size: a.Size, IsInline: a.IsInline}
}

func (m messageDTO) raw() domain.RawMessage {
	r := domain.RawMessage{
		MessageSummary:    m.summary(),
		Cc:                toAddresses(m.CcRecipients),
		Bcc:               toAddresses(m.BccRecipients),
		InternetMessageID: m.InternetMessageID,
		ParentFolderID:    m.ParentFolderID,
		ReplyTo:           m.ReplyToAddresses(),
	}
	if sa, ok := m.SenderAddress(); ok {
		r.Sender = sa
	}
	if m.Body != nil {
		r.Body = domain.RawBody{Format: bodyFormat(m.Body.ContentType), Content: m.Body.Content}
	}
	for _, a := range m.Attachments {
		r.Attachments = append(r.Attachments, a.toDomain())
	}
	for _, h := range m.InternetMessageHeaders {
		r.InternetHeaders = append(r.InternetHeaders, domain.Header{Name: h.Name, Value: h.Value})
	}
	return r
}

// bodyFormat maps Graph's contentType. Anything but "text" is treated as HTML
// so the use case converts it rather than showing markup as text.
func bodyFormat(ct string) domain.BodyFormat {
	if strings.EqualFold(ct, "text") {
		return domain.BodyText
	}
	return domain.BodyHTML
}

// Write DTOs.

type outgoingDTO struct {
	Subject                string         `json:"subject"`
	Body                   bodyDTO        `json:"body"`
	ToRecipients           []recipientDTO `json:"toRecipients"`
	CcRecipients           []recipientDTO `json:"ccRecipients,omitempty"`
	BccRecipients          []recipientDTO `json:"bccRecipients,omitempty"`
	InternetMessageHeaders []headerDTO    `json:"internetMessageHeaders,omitempty"`
}

func fromAddresses(as []domain.Address) []recipientDTO {
	out := make([]recipientDTO, 0, len(as))
	for _, a := range as {
		out = append(out, recipientDTO{EmailAddress: emailAddressDTO{Name: a.Name, Address: a.Address}})
	}
	return out
}

func outgoing(m domain.OutgoingMessage) outgoingDTO {
	d := outgoingDTO{
		Subject:       m.Subject,
		Body:          bodyDTO{ContentType: "Text", Content: m.Body},
		ToRecipients:  fromAddresses(m.To),
		CcRecipients:  fromAddresses(m.Cc),
		BccRecipients: fromAddresses(m.Bcc),
	}
	for _, h := range m.InternetHeaders {
		d.InternetMessageHeaders = append(d.InternetMessageHeaders, headerDTO{Name: h.Name, Value: h.Value})
	}
	return d
}
