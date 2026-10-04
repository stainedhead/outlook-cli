package domain

import (
	"html"
	"net/url"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// This file holds the untrusted-content transforms of PRD section 10: HTML to
// text, link extraction and defanging, control-character stripping and byte
// bounds. Everything here is pure and treats its input as hostile.

// CleanText removes characters that can hide or reorder text: C0/C1 controls
// other than newline and tab, every Unicode format character (category Cf:
// bidi controls, zero-width and joiner characters, the BOM, soft hyphen,
// U+061C, U+180E and the tag block), variation selectors, and the line and
// paragraph separators U+2028/U+2029. Removing ZWJ splits emoji ZWJ sequences
// into their component emoji; that is deliberate. CR is dropped; CRLF becomes
// LF.
func CleanText(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\n' || r == '\t':
			b.WriteRune(r)
		case r == '\r', r == utf8.RuneError:
			// dropped (invalid UTF-8 becomes RuneError)
		case unicode.IsControl(r):
		case unicode.Is(unicode.Cf, r), isHiddenRune(r):
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// isHiddenRune reports non-Cf runes that render invisibly or break lines
// (FR-R5): variation selectors, the unassigned tag-block start and U+2028/9.
func isHiddenRune(r rune) bool {
	return r >= 0xFE00 && r <= 0xFE0F || r >= 0xE0100 && r <= 0xE01EF ||
		r >= 0xE0000 && r <= 0xE007F || r == 0x2028 || r == 0x2029
}

// TruncateBytes cuts s to at most max bytes on a rune boundary and reports
// whether it cut. max <= 0 returns "" and true for a non-empty s.
func TruncateBytes(s string, max int) (string, bool) {
	if len(s) <= max {
		return s, false
	}
	if max <= 0 {
		return "", true
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut], true
}

var urlRe = regexp.MustCompile(`(?i)\b(?:https?|ftp)://[^\s<>"'\x60]+`)

// DefangURL rewrites the scheme so the URL is not clickable or fetchable by
// accident: http -> hxxp, https -> hxxps, ftp -> fxp. Host and path are left
// readable (PRD section 10). Other strings are returned unchanged.
func DefangURL(u string) string {
	l := strings.ToLower(u)
	switch {
	case strings.HasPrefix(l, "https://"):
		return "hxxps" + u[len("https"):]
	case strings.HasPrefix(l, "http://"):
		return "hxxp" + u[len("http"):]
	case strings.HasPrefix(l, "ftp://"):
		return "fxp" + u[len("ftp"):]
	}
	return u
}

// DefangText defangs every URL found inside free text.
func DefangText(s string) string {
	return urlRe.ReplaceAllStringFunc(s, DefangURL)
}

// URLDomain returns the lower-case host of an http, https or ftp URL, or ""
// when u is not such a URL.
func URLDomain(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https", "ftp":
		return strings.ToLower(u.Hostname())
	}
	return ""
}

func makeLink(text, rawURL string, defang bool) (Link, bool) {
	rawURL = strings.TrimSpace(CleanText(rawURL))
	d := URLDomain(rawURL)
	if d == "" {
		return Link{}, false
	}
	text = collapseSpace(CleanText(text))
	if len(text) > 200 {
		text, _ = TruncateBytes(text, 200)
	}
	if defang {
		text = DefangText(text)
		rawURL = DefangURL(rawURL)
	}
	return Link{Text: text, URL: rawURL, Domain: d}, true
}

// LinksFromText lists the bare URLs of a plain-text body (Text is empty).
// Duplicates are dropped; at most MaxLinks are returned.
func LinksFromText(s string, defang bool) []Link {
	var out []Link
	seen := map[string]bool{}
	for _, m := range urlRe.FindAllString(s, -1) {
		m = strings.TrimRight(m, ".,;:!?)]}>")
		if seen[m] {
			continue
		}
		seen[m] = true
		if l, ok := makeLink("", m, defang); ok {
			out = append(out, l)
			if len(out) == MaxLinks {
				break
			}
		}
	}
	return out
}

func collapseSpace(s string) string { return strings.Join(strings.Fields(s), " ") }

// Elements whose whole content is dropped.
var skipElements = map[string]bool{
	"script": true, "style": true, "head": true, "title": true, "template": true,
	"noscript": true, "svg": true, "iframe": true, "object": true, "embed": true,
	"canvas": true, "audio": true, "video": true, "math": true, "select": true,
	"textarea": true, "button": true,
}

// Elements that start a new line (before and after).
var blockElements = map[string]bool{
	"p": true, "div": true, "section": true, "article": true, "header": true,
	"footer": true, "nav": true, "aside": true, "main": true, "ul": true,
	"ol": true, "table": true, "tr": true, "blockquote": true, "pre": true,
	"h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
	"hr": true, "form": true, "fieldset": true, "address": true, "dl": true,
	"dt": true, "dd": true, "figure": true, "figcaption": true, "li": true,
}

var voidElements = map[string]bool{
	"br": true, "hr": true, "img": true, "input": true, "meta": true, "link": true,
	"area": true, "base": true, "col": true, "embed": true, "source": true,
	"track": true, "wbr": true, "param": true,
}

var (
	hiddenStyleRe = regexp.MustCompile(`(?i)display\s*:\s*none|visibility\s*:\s*hidden|font-size\s*:\s*0(?:px|pt|em|%)?\s*(?:;|$)|opacity\s*:\s*0(?:\.0+)?\s*(?:;|$)`)
	spaceRunRe    = regexp.MustCompile(`[ \t]+`)
	blankRunsRe   = regexp.MustCompile(`\n{3,}`)
)

type htmlTag struct {
	name    string
	closing bool
	selfEnd bool
	attrs   map[string]string
}

// HTMLToText converts untrusted HTML to plain text and lists its links.
// Nothing is fetched. Script, style, head, comments, embedded content and
// elements hidden with hidden/display:none/visibility:hidden/zero font size
// or opacity are dropped; images contribute nothing (no alt text, no fetch);
// anchors keep their text and are listed in the returned links (http, https
// and ftp only; javascript:, data: and other schemes are dropped). When
// defang is true, link URLs and URLs inside the text are defanged. The result
// is cleaned with CleanText. The parser is a small hand-written tokenizer: it
// never panics and never recurses on input depth.
func HTMLToText(src string, defang bool) (string, []Link) {
	var (
		out      strings.Builder
		links    []Link
		seenLink = map[string]bool{}
		skipName string // element whose content is being skipped
		skipDeep int
		anchor   *struct {
			href string
			text strings.Builder
		}
	)
	emit := func(s string) {
		if anchor != nil {
			anchor.text.WriteString(s)
		}
		out.WriteString(s)
	}
	closeAnchor := func() {
		if anchor == nil {
			return
		}
		if len(links) < MaxLinks {
			if l, ok := makeLink(anchor.text.String(), anchor.href, defang); ok && !seenLink[l.URL+"\x00"+l.Text] {
				seenLink[l.URL+"\x00"+l.Text] = true
				links = append(links, l)
			}
		}
		anchor = nil
	}

	i := 0
	for i < len(src) {
		lt := strings.IndexByte(src[i:], '<')
		if lt < 0 {
			if skipName == "" {
				emit(html.UnescapeString(src[i:]))
			}
			break
		}
		if lt > 0 && skipName == "" {
			emit(html.UnescapeString(src[i : i+lt]))
		}
		i += lt
		rest := src[i:]
		switch {
		case strings.HasPrefix(rest, "<!--"):
			end := strings.Index(rest[4:], "-->")
			if end < 0 {
				i = len(src)
			} else {
				i += 4 + end + 3
			}
			continue
		case len(rest) > 1 && (rest[1] == '!' || rest[1] == '?'):
			end := strings.IndexByte(rest, '>')
			if end < 0 {
				i = len(src)
			} else {
				i += end + 1
			}
			continue
		case len(rest) > 1 && (rest[1] == '/' || isLetter(rest[1])):
		default:
			if skipName == "" {
				emit("<")
			}
			i++
			continue
		}
		tag, n := parseTag(rest)
		i += n
		if tag.name == "" {
			continue
		}
		if skipName != "" {
			if tag.name == skipName && !tag.selfEnd && !voidElements[tag.name] {
				if tag.closing {
					skipDeep--
					if skipDeep == 0 {
						skipName = ""
					}
				} else {
					skipDeep++
				}
			}
			continue
		}
		if tag.closing {
			switch {
			case tag.name == "a":
				closeAnchor()
			case blockElements[tag.name] && tag.name != "li":
				emit("\n")
			case tag.name == "td" || tag.name == "th":
				emit("\t")
			}
			continue
		}
		hidden := false
		if _, ok := tag.attrs["hidden"]; ok {
			hidden = true
		}
		if st, ok := tag.attrs["style"]; ok && hiddenStyleRe.MatchString(st) {
			hidden = true
		}
		if strings.EqualFold(tag.attrs["aria-hidden"], "true") && tag.name != "a" {
			hidden = true
		}
		if (skipElements[tag.name] || hidden) && !voidElements[tag.name] {
			if !tag.selfEnd {
				skipName, skipDeep = tag.name, 1
			}
			continue
		}
		switch {
		case tag.name == "br":
			emit("\n")
		case tag.name == "a":
			closeAnchor()
			if h, ok := tag.attrs["href"]; ok {
				anchor = &struct {
					href string
					text strings.Builder
				}{href: html.UnescapeString(h)}
			}
		case tag.name == "li":
			emit("\n- ")
		case blockElements[tag.name]:
			emit("\n")
		}
	}
	closeAnchor()

	text := CleanText(out.String())
	lines := strings.Split(text, "\n")
	for k, ln := range lines {
		lines[k] = strings.TrimSpace(spaceRunRe.ReplaceAllString(strings.ReplaceAll(ln, " ", " "), " "))
	}
	text = strings.TrimSpace(blankRunsRe.ReplaceAllString(strings.Join(lines, "\n"), "\n\n"))
	if defang {
		text = DefangText(text)
	}
	return text, links
}

func isLetter(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }

// parseTag parses a tag starting at s[0]=='<' and returns it with the number
// of bytes consumed. An unterminated tag consumes the rest of the input and
// yields an empty name.
func parseTag(s string) (htmlTag, int) {
	i := 1
	t := htmlTag{attrs: map[string]string{}}
	if i < len(s) && s[i] == '/' {
		t.closing = true
		i++
	}
	start := i
	for i < len(s) && !isSpaceByte(s[i]) && s[i] != '>' && s[i] != '/' {
		i++
	}
	t.name = strings.ToLower(s[start:i])
	for i < len(s) {
		for i < len(s) && (isSpaceByte(s[i]) || s[i] == '/') {
			if s[i] == '/' && i+1 < len(s) && s[i+1] == '>' {
				t.selfEnd = true
			}
			i++
		}
		if i >= len(s) {
			break
		}
		if s[i] == '>' {
			return t, i + 1
		}
		ns := i
		for i < len(s) && !isSpaceByte(s[i]) && s[i] != '=' && s[i] != '>' && s[i] != '/' {
			i++
		}
		name := strings.ToLower(s[ns:i])
		for i < len(s) && isSpaceByte(s[i]) {
			i++
		}
		val := ""
		if i < len(s) && s[i] == '=' {
			i++
			for i < len(s) && isSpaceByte(s[i]) {
				i++
			}
			if i < len(s) && (s[i] == '"' || s[i] == '\'') {
				q := s[i]
				i++
				vs := i
				for i < len(s) && s[i] != q {
					i++
				}
				val = s[vs:i]
				if i < len(s) {
					i++
				}
			} else {
				vs := i
				for i < len(s) && !isSpaceByte(s[i]) && s[i] != '>' {
					i++
				}
				val = s[vs:i]
			}
		}
		if name != "" {
			if _, dup := t.attrs[name]; !dup {
				t.attrs[name] = val
			}
		}
	}
	return htmlTag{}, len(s)
}

func isSpaceByte(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' }

var authResultRe = regexp.MustCompile(`(?i)\b(spf|dkim|dmarc)\s*=\s*([a-z]+)`)

// AuthUnverified is the verdict reported for a mechanism when no
// Authentication-Results header from a trusted authserv-id supplies one.
const AuthUnverified = "unverified"

// ParseAuthResults extracts SPF, DKIM and DMARC verdicts from every
// Authentication-Results header regardless of who wrote it, taking the first
// verdict per mechanism. A sender can inject such a header, so this is only
// safe for tests and tools; product code uses ParseAuthResultsFor (FR-R10).
//
// ASSUMPTION(unverified against a real tenant): Graph exposes the header via
// internetMessageHeaders and its format follows RFC 8601.
func ParseAuthResults(headers []Header) *AuthResults { return parseAuth(headers, nil, false) }

// ParseAuthResultsFor is the verified form of ParseAuthResults: only headers
// whose authserv-id (the token before the first ';', RFC 8601) equals one of
// trustedIDs (case-insensitive) count. A mechanism without a verdict from such
// a header reports AuthUnverified. It returns nil only when there is no
// Authentication-Results header at all. With no trustedIDs every verdict is
// unverified. Advisory only.
func ParseAuthResultsFor(headers []Header, trustedIDs []string) *AuthResults {
	return parseAuth(headers, trustedIDs, true)
}

func parseAuth(headers []Header, trustedIDs []string, verify bool) *AuthResults {
	var res AuthResults
	present := false
	for _, h := range headers {
		if !strings.EqualFold(h.Name, "Authentication-Results") {
			continue
		}
		present = true
		if verify {
			id, _, _ := strings.Cut(h.Value, ";")
			id = strings.TrimSpace(id)
			if id == "" || !containsFold(trustedIDs, id) {
				continue
			}
		}
		for _, m := range authResultRe.FindAllStringSubmatch(h.Value, -1) {
			v := strings.ToLower(m[2])
			switch strings.ToLower(m[1]) {
			case "spf":
				if res.SPF == "" {
					res.SPF = v
				}
			case "dkim":
				if res.DKIM == "" {
					res.DKIM = v
				}
			case "dmarc":
				if res.DMARC == "" {
					res.DMARC = v
				}
			}
		}
	}
	if verify {
		for _, p := range []*string{&res.SPF, &res.DKIM, &res.DMARC} {
			if *p == "" {
				*p = AuthUnverified
			}
		}
		if !present {
			return nil
		}
		return &res
	}
	if res.SPF == "" && res.DKIM == "" && res.DMARC == "" {
		return nil
	}
	return &res
}
