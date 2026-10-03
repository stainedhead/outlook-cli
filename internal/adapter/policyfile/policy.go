package policyfile

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/goccy/go-yaml"

	"github.com/stainedhead/outlook-cli/internal/domain"
)

// maxPolicyBytes bounds the policy file read.
const maxPolicyBytes = 1 << 20

// Defaults applied when the file omits a bound. Omitting never widens access:
// the domain treats a zero bound as "deny" or "unbounded", so the loader
// resolves it to the PRD section 9 sample values instead.
const (
	DefaultMaxWritesPerRun  = 20
	DefaultMaxBodyBytes     = 16000
	DefaultSendBodyMaxBytes = 20000
	DefaultMaxResults       = 100
)

const (
	valAllow = "allow"
	valDeny  = "deny"
)

// The YAML shape of PRD section 9. Pointers distinguish "omitted" where the
// difference matters; everything omitted resolves to the denying value.
type fileDoc struct {
	Profile         string   `yaml:"profile"`
	Mailbox         string   `yaml:"mailbox"`
	InternalDomains []string `yaml:"internal_domains"`
	Read            struct {
		Folders      []string `yaml:"folders"`
		MaxBodyBytes int      `yaml:"max_body_bytes"`
		HTMLToText   *bool    `yaml:"html_to_text"`
		DefangLinks  *bool    `yaml:"defang_links"`
		Attachments  struct {
			Download   string   `yaml:"download"`
			AllowTypes []string `yaml:"allow_types"`
			MaxBytes   int64    `yaml:"max_bytes"`
			OutDir     string   `yaml:"out_dir"`
		} `yaml:"attachments"`
	} `yaml:"read"`
	Send struct {
		Mode       string `yaml:"mode"`
		Recipients struct {
			AllowDomains   []string `yaml:"allow_domains"`
			AllowAddresses []string `yaml:"allow_addresses"`
			External       string   `yaml:"external"`
			MaxTotal       int      `yaml:"max_total"`
			Bcc            string   `yaml:"bcc"`
		} `yaml:"recipients"`
		ReplyAll      string `yaml:"reply_all"`
		Attachments   string `yaml:"attachments"`
		SubjectPrefix string `yaml:"subject_prefix"`
		Footer        string `yaml:"footer"`
		Body          struct {
			MaxBytes int `yaml:"max_bytes"`
		} `yaml:"body"`
		ContentFilters []string `yaml:"content_filters"`
		Rate           struct {
			PerHour int `yaml:"per_hour"`
			PerDay  int `yaml:"per_day"`
		} `yaml:"rate"`
	} `yaml:"send"`
	Limits struct {
		MaxResults      int  `yaml:"max_results"`
		MaxWritesPerRun *int `yaml:"max_writes_per_run"`
	} `yaml:"limits"`
	Audit struct {
		Path string `yaml:"path"`
	} `yaml:"audit"`
}

const hint = "fix the policy file (see the sample policy in the user docs); a policy that cannot be loaded blocks every command"

func invalid(format string, a ...any) error {
	return domain.NewValidation(fmt.Sprintf("policy invalid: "+format, a...)).WithHint(hint)
}

// Parse validates policy YAML and returns the typed policy. It never reads the
// file system. Error text names keys and problems, never values from the file.
func Parse(data []byte) (domain.Policy, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return domain.Policy{}, invalid("file is empty")
	}
	var doc fileDoc
	dec := yaml.NewDecoder(bytes.NewReader(data), yaml.Strict())
	if err := dec.Decode(&doc); err != nil {
		if errors.Is(err, io.EOF) {
			return domain.Policy{}, invalid("file is empty")
		}
		return domain.Policy{}, invalid("%s", firstLine(err.Error()))
	}
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return domain.Policy{}, invalid("exactly one YAML document is allowed")
	}
	return convert(doc)
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if strings.Contains(s, "already defined") {
		s = "duplicate key: " + s
	}
	return s
}

type problems []string

func (p *problems) add(format string, a ...any) { *p = append(*p, fmt.Sprintf(format, a...)) }

func convert(d fileDoc) (domain.Policy, error) {
	var pr problems
	p := domain.Policy{
		Profile:   strings.TrimSpace(d.Profile),
		Mailbox:   strings.ToLower(strings.TrimSpace(d.Mailbox)),
		AuditPath: strings.TrimSpace(d.Audit.Path),
	}
	if p.Profile == "" {
		pr.add("profile is required")
	}
	if !validAddress(p.Mailbox) {
		pr.add("mailbox must be an address like name@domain")
	}
	p.InternalDomains = domains(&pr, "internal_domains", d.InternalDomains)
	if len(p.InternalDomains) == 0 {
		pr.add("internal_domains must list at least one domain")
	}
	if p.AuditPath == "" || !filepath.IsAbs(p.AuditPath) {
		pr.add("audit.path must be an absolute path")
	}

	convertRead(&pr, &p, d)
	convertSend(&pr, &p, d)

	if d.Limits.MaxResults < 0 {
		pr.add("limits.max_results must not be negative")
	}
	writes := DefaultMaxWritesPerRun
	if d.Limits.MaxWritesPerRun != nil {
		writes = *d.Limits.MaxWritesPerRun
	}
	if writes < 0 {
		pr.add("limits.max_writes_per_run must not be negative")
	}
	results := d.Limits.MaxResults
	if results == 0 {
		results = DefaultMaxResults
	}
	p.Limits = domain.Limits{MaxResults: results, MaxWritesPerRun: writes}

	if len(pr) > 0 {
		return domain.Policy{}, invalid("%s", strings.Join(pr, "; "))
	}
	return p, nil
}

func convertRead(pr *problems, p *domain.Policy, d fileDoc) {
	r := &p.Read
	for _, f := range d.Read.Folders {
		f = strings.TrimSpace(f)
		if f == "" || strings.ContainsAny(f, "\r\n\x00") {
			pr.add("read.folders entries must be non-empty names")
			continue
		}
		r.Folders = append(r.Folders, f)
	}
	if d.Read.MaxBodyBytes < 0 {
		pr.add("read.max_body_bytes must not be negative")
	}
	r.MaxBodyBytes = d.Read.MaxBodyBytes
	if r.MaxBodyBytes == 0 {
		r.MaxBodyBytes = DefaultMaxBodyBytes
	}
	r.HTMLToText = d.Read.HTMLToText == nil || *d.Read.HTMLToText
	r.DefangLinks = d.Read.DefangLinks == nil || *d.Read.DefangLinks

	a := d.Read.Attachments
	hasDetail := len(a.AllowTypes) > 0 || a.MaxBytes != 0 || a.OutDir != ""
	switch strings.ToLower(strings.TrimSpace(a.Download)) {
	case valDeny:
		if hasDetail {
			pr.add("read.attachments.download is deny but allow_types, max_bytes or out_dir are set")
		}
		return
	case "", valAllow:
		if !hasDetail {
			if a.Download != "" {
				pr.add("read.attachments.download allow needs allow_types, max_bytes and out_dir")
			}
			return
		}
	default:
		pr.add("read.attachments.download must be deny or omitted")
		return
	}
	var types []string
	for _, t := range a.AllowTypes {
		t = strings.ToLower(strings.TrimSpace(t))
		if t == "" || strings.ContainsAny(t, "./\\ \x00") {
			pr.add("read.attachments.allow_types entries must be bare extensions such as pdf")
			continue
		}
		types = append(types, t)
	}
	if len(a.AllowTypes) == 0 {
		pr.add("read.attachments.allow_types must list at least one type")
	}
	if a.MaxBytes <= 0 {
		pr.add("read.attachments.max_bytes must be positive")
	}
	if !filepath.IsAbs(a.OutDir) {
		pr.add("read.attachments.out_dir must be an absolute path")
	}
	r.Attachments = domain.AttachmentPolicy{Download: true, AllowTypes: types, MaxBytes: a.MaxBytes, OutDir: filepath.Clean(a.OutDir)}
}

func convertSend(pr *problems, p *domain.Policy, d fileDoc) {
	s := &p.Send
	switch strings.ToLower(strings.TrimSpace(d.Send.Mode)) {
	case "", valDeny:
		s.Mode = domain.SendDeny
	case valAllow:
		s.Mode = domain.SendAllow
	case string(domain.SendDryRunOnly):
		s.Mode = domain.SendDryRunOnly
	default:
		pr.add("send.mode must be allow, dry_run_only or deny")
	}
	rc := d.Send.Recipients
	switch strings.ToLower(strings.TrimSpace(rc.External)) {
	case "", valDeny:
		s.Recipients.External = domain.ExternalDeny
	case string(domain.ExternalDraftOnly):
		s.Recipients.External = domain.ExternalDraftOnly
	case valAllow:
		s.Recipients.External = domain.ExternalAllow
	default:
		pr.add("send.recipients.external must be deny, draft_only or allow")
	}
	s.Recipients.AllowDomains = domains(pr, "send.recipients.allow_domains", rc.AllowDomains)
	for _, a := range rc.AllowAddresses {
		a = strings.ToLower(strings.TrimSpace(a))
		if !validAddress(a) {
			pr.add("send.recipients.allow_addresses entries must be addresses")
			continue
		}
		s.Recipients.AllowAddresses = append(s.Recipients.AllowAddresses, a)
	}
	if rc.MaxTotal < 0 {
		pr.add("send.recipients.max_total must not be negative")
	}
	if rc.MaxTotal < 1 && (s.Mode == domain.SendAllow || s.Mode == domain.SendDryRunOnly) {
		pr.add("send.recipients.max_total must be at least 1 when send.mode is allow or dry_run_only")
	}
	s.Recipients.MaxTotal = rc.MaxTotal
	s.Recipients.BccAllowed = allowDeny(pr, "send.recipients.bcc", rc.Bcc)
	s.ReplyAll = allowDeny(pr, "send.reply_all", d.Send.ReplyAll)
	s.Attachments = allowDeny(pr, "send.attachments", d.Send.Attachments)
	s.SubjectPrefix = d.Send.SubjectPrefix
	s.Footer = d.Send.Footer
	if d.Send.Body.MaxBytes < 0 {
		pr.add("send.body.max_bytes must not be negative")
	}
	s.BodyMaxBytes = d.Send.Body.MaxBytes
	if s.BodyMaxBytes == 0 {
		s.BodyMaxBytes = DefaultSendBodyMaxBytes
	}
	seen := map[string]bool{}
	known := map[string]bool{}
	for _, k := range KnownFilters() {
		known[k] = true
	}
	for _, f := range d.Send.ContentFilters {
		f = strings.TrimSpace(f)
		switch {
		case !known[f]:
			pr.add("send.content_filters has unknown filter %q (known: %s)", f, strings.Join(KnownFilters(), ", "))
		case seen[f]:
			pr.add("send.content_filters has duplicate filter %q", f)
		default:
			s.ContentFilters = append(s.ContentFilters, f)
		}
		seen[f] = true
	}
	if d.Send.Rate.PerHour < 0 {
		pr.add("send.rate.per_hour must not be negative")
	}
	if d.Send.Rate.PerDay < 0 {
		pr.add("send.rate.per_day must not be negative")
	}
	s.Rate = domain.SendRate{PerHour: d.Send.Rate.PerHour, PerDay: d.Send.Rate.PerDay}
}

func allowDeny(pr *problems, key, v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", valDeny:
		return false
	case valAllow:
		return true
	}
	pr.add("%s must be allow or deny", key)
	return false
}

func domains(pr *problems, key string, in []string) []string {
	var out []string
	for _, d := range in {
		d = strings.ToLower(strings.TrimSpace(d))
		if d == "" || strings.ContainsAny(d, "@ \t\r\n") || strings.Trim(d, ".") != d {
			pr.add("%s entries must be bare domains such as example.com", key)
			continue
		}
		out = append(out, d)
	}
	return out
}

func validAddress(a string) bool {
	i := strings.IndexByte(a, '@')
	if i <= 0 || i == len(a)-1 || strings.Count(a, "@") != 1 {
		return false
	}
	return !strings.ContainsAny(a, " \t\r\n<>,;\"")
}

// Option configures Load.
type Option func(*loadConfig)

type loadConfig struct {
	allowWritable bool
	access        func(string) (bool, error)
}

// AllowWritable disables the refusal of a policy file or directory the current
// user can write. It exists for development and tests; production composition
// must not set it.
func AllowWritable() Option { return func(c *loadConfig) { c.allowWritable = true } }

func withAccess(f func(string) (bool, error)) Option {
	return func(c *loadConfig) { c.access = f }
}

// Load reads and parses the policy file at path. Unless AllowWritable is set it
// refuses (policy_denied, exit 6) a file or containing directory that the
// current user can write, because the agent must not be able to edit its own
// guardrails.
func Load(path string, opts ...Option) (domain.Policy, error) {
	cfg := loadConfig{access: writableByMe}
	for _, o := range opts {
		o(&cfg)
	}
	f, err := os.Open(path)
	if err != nil {
		return domain.Policy{}, domain.NewValidation("policy: cannot read " + path).WithHint(hint).WithCause(err)
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, maxPolicyBytes+1))
	if err != nil {
		return domain.Policy{}, domain.NewValidation("policy: cannot read " + path).WithHint(hint).WithCause(err)
	}
	if len(data) > maxPolicyBytes {
		return domain.Policy{}, invalid("file exceeds %d bytes", maxPolicyBytes)
	}
	if !cfg.allowWritable {
		for _, t := range []struct{ what, path string }{{"file", path}, {"directory", filepath.Dir(path)}} {
			w, err := cfg.access(t.path)
			if err != nil {
				return domain.Policy{}, domain.NewPolicyDenied("policy: cannot check permissions of " + t.what + " " + t.path).
					WithHint("the policy file and its directory must be checkable and read-only for the agent user").WithCause(err)
			}
			if w {
				return domain.Policy{}, domain.NewPolicyDenied("policy " + t.what + " " + t.path + " is writable by the current user").
					WithHint("install the policy where only an administrator can change it (root-owned, agent user read-only)")
			}
		}
	}
	return Parse(data)
}

// Provider implements usecase.PolicyProvider over a policy file. The first
// result, success or failure, is cached for the life of the process, so a
// file edited mid-run cannot change the rules of a run in progress.
type Provider struct {
	path string
	opts []Option
	once sync.Once
	pol  domain.Policy
	err  error
}

// NewProvider returns a Provider for path.
func NewProvider(path string, opts ...Option) *Provider {
	return &Provider{path: path, opts: opts}
}

// Policy returns the loaded policy. Callers get their own copy of every slice.
func (p *Provider) Policy(ctx context.Context) (domain.Policy, error) {
	if err := ctx.Err(); err != nil {
		return domain.Policy{}, err
	}
	p.once.Do(func() { p.pol, p.err = Load(p.path, p.opts...) })
	if p.err != nil {
		return domain.Policy{}, p.err
	}
	return clone(p.pol), nil
}

func clone(p domain.Policy) domain.Policy {
	cp := func(s []string) []string {
		if s == nil {
			return nil
		}
		return append([]string{}, s...)
	}
	p.InternalDomains = cp(p.InternalDomains)
	p.Read.Folders = cp(p.Read.Folders)
	p.Read.Attachments.AllowTypes = cp(p.Read.Attachments.AllowTypes)
	p.Send.Recipients.AllowDomains = cp(p.Send.Recipients.AllowDomains)
	p.Send.Recipients.AllowAddresses = cp(p.Send.Recipients.AllowAddresses)
	p.Send.ContentFilters = cp(p.Send.ContentFilters)
	return p
}
