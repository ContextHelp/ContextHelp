package upgrade

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// HeaderName is the response header dpkms sets on every /api/v1 response
// while an upgrade is not idle. Clients render the upgrade banner from it,
// so the banner follows the instance a command talks to, local or remote.
const HeaderName = "X-Dpkms-Upgrade"

// maxHeaderError bounds the error text the header carries; the full text
// stays on /healthz and `ctxt upgrade status`.
const maxHeaderError = 256

// ErrHeader wraps every DecodeHeader failure.
var ErrHeader = errors.New("upgrade: malformed " + HeaderName + " header")

// EncodeHeader renders st as the header value: an RFC 8941 dictionary
// carrying what the banner shows. Idle (or zero) status encodes to "".
//
//	state=in_progress, bucket=reingest_selective, done=47, total=120, eta=32
//	state=failed, bucket=reindex_auto, done=2, total=5, eta=0, error="disk full"
//
// Progress is not sent; DecodeHeader derives it from done and total.
func EncodeHeader(st Status) string {
	if st.State == StateIdle || st.State == "" {
		return ""
	}
	members := []string{"state=" + sfItem(string(st.State))}
	if st.Bucket != "" {
		members = append(members, "bucket="+sfItem(string(st.Bucket)))
	}
	if st.Target != "" {
		members = append(members, "target="+sfString(st.Target))
	}
	members = append(members, "done="+strconv.Itoa(st.Done), "total="+strconv.Itoa(st.Total))
	if st.Failed > 0 {
		members = append(members, "failed="+strconv.Itoa(st.Failed))
	}
	members = append(members, "eta="+strconv.Itoa(st.EtaSeconds))
	if st.LastError != "" {
		msg := st.LastError
		if len(msg) > maxHeaderError {
			msg = msg[:maxHeaderError]
		}
		members = append(members, "error="+sfString(msg))
	}
	return strings.Join(members, ", ")
}

// DecodeHeader parses a header value EncodeHeader produced. Unknown
// members, parameters and item types are skipped so the server can grow
// the value. A value that is not a dictionary, or whose state is missing
// or idle, is an ErrHeader.
func DecodeHeader(v string) (Status, error) {
	dict, err := parseDictionary(v)
	if err != nil {
		return Status{}, err
	}
	var st Status
	texts := map[string]*string{
		"state": (*string)(&st.State), "bucket": (*string)(&st.Bucket),
		"target": &st.Target, "error": &st.LastError,
	}
	ints := map[string]*int{"done": &st.Done, "total": &st.Total, "failed": &st.Failed, "eta": &st.EtaSeconds}
	for key, it := range dict {
		if dst, ok := texts[key]; ok {
			if it.kind != kindText {
				return Status{}, fmt.Errorf("%w: %s is not a string or token", ErrHeader, key)
			}
			*dst = it.str
		}
		if dst, ok := ints[key]; ok {
			if it.kind != kindInt {
				return Status{}, fmt.Errorf("%w: %s is not an integer", ErrHeader, key)
			}
			*dst = it.num
		}
	}
	if st.State == "" || st.State == StateIdle {
		return Status{}, fmt.Errorf("%w: no active state in %q", ErrHeader, v)
	}
	if st.Total > 0 {
		st.Progress = min(float64(st.Done)/float64(st.Total), 1)
	}
	return st, nil
}

// sfItem renders s as an sf-token when it is one, else as an sf-string.
func sfItem(s string) string {
	if isToken(s) {
		return s
	}
	return sfString(s)
}

// sfString renders s as an sf-string. Bytes outside printable ASCII
// become '?', so the value can never break the header line or reach a
// terminal as a control sequence.
func sfString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '"' || c == '\\':
			b.WriteByte('\\')
			b.WriteByte(c)
		case c < 0x20 || c > 0x7e:
			b.WriteByte('?')
		default:
			b.WriteByte(c)
		}
	}
	b.WriteByte('"')
	return b.String()
}

func isToken(s string) bool {
	if s == "" || (!isAlpha(s[0]) && s[0] != '*') {
		return false
	}
	for i := 1; i < len(s); i++ {
		if !isTokenChar(s[i]) {
			return false
		}
	}
	return true
}

func isAlpha(c byte) bool { return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') }
func isDigit(c byte) bool { return c >= '0' && c <= '9' }
func isLower(c byte) bool { return c >= 'a' && c <= 'z' }

// isTokenChar is RFC 9110 tchar plus ':' and '/' (RFC 8941 sf-token).
func isTokenChar(c byte) bool {
	return isAlpha(c) || isDigit(c) || strings.IndexByte("!#$%&'*+-.^_`|~:/", c) >= 0
}

// itemKind tells the decoded bare-item types apart.
type itemKind int

const (
	kindOther itemKind = iota // boolean, decimal, byte sequence, or no value
	kindText                  // string or token
	kindInt                   // integer
)

// item is one decoded bare item: str for strings and tokens, num for
// integers.
type item struct {
	kind itemKind
	str  string
	num  int
}

// parser walks an RFC 8941 dictionary (§4.2.2) and keeps the members
// whose values are tokens, strings or integers.
type parser struct {
	s   string
	pos int
}

func parseDictionary(v string) (map[string]item, error) {
	p := &parser{s: v}
	out := map[string]item{}
	p.skipSP()
	if p.eof() {
		return nil, fmt.Errorf("%w: empty value", ErrHeader)
	}
	for {
		key, err := p.key()
		if err != nil {
			return nil, err
		}
		var it item
		if p.peek() == '=' {
			p.pos++
			if it, err = p.bareItem(); err != nil {
				return nil, err
			}
		}
		if err := p.skipParams(); err != nil {
			return nil, err
		}
		out[key] = it
		p.skipOWS()
		if p.eof() {
			return out, nil
		}
		if p.peek() != ',' {
			return nil, p.fail("expected ','")
		}
		p.pos++
		p.skipOWS()
		if p.eof() {
			return nil, p.fail("trailing ','")
		}
	}
}

func (p *parser) eof() bool { return p.pos >= len(p.s) }

func (p *parser) peek() byte {
	if p.eof() {
		return 0
	}
	return p.s[p.pos]
}

func (p *parser) fail(msg string) error {
	return fmt.Errorf("%w: %s at offset %d in %q", ErrHeader, msg, p.pos, p.s)
}

func (p *parser) skipSP() {
	for !p.eof() && p.s[p.pos] == ' ' {
		p.pos++
	}
}

func (p *parser) skipOWS() {
	for !p.eof() && (p.s[p.pos] == ' ' || p.s[p.pos] == '\t') {
		p.pos++
	}
}

// key parses an sf key: lcalpha or '*', then lcalpha, digit, '_', '-',
// '.' or '*'.
func (p *parser) key() (string, error) {
	start := p.pos
	if c := p.peek(); !isLower(c) && c != '*' {
		return "", p.fail("expected a key")
	}
	p.pos++
	for !p.eof() {
		c := p.s[p.pos]
		if !isLower(c) && !isDigit(c) && strings.IndexByte("_-.*", c) < 0 {
			break
		}
		p.pos++
	}
	return p.s[start:p.pos], nil
}

func (p *parser) skipParams() error {
	for p.peek() == ';' {
		p.pos++
		p.skipSP()
		if _, err := p.key(); err != nil {
			return err
		}
		if p.peek() == '=' {
			p.pos++
			if _, err := p.bareItem(); err != nil {
				return err
			}
		}
	}
	return nil
}

func (p *parser) bareItem() (item, error) {
	switch c := p.peek(); {
	case c == '-' || isDigit(c):
		return p.number()
	case c == '"':
		s, err := p.str()
		return item{kind: kindText, str: s}, err
	case isAlpha(c) || c == '*':
		start := p.pos
		for !p.eof() && isTokenChar(p.s[p.pos]) {
			p.pos++
		}
		return item{kind: kindText, str: p.s[start:p.pos]}, nil
	case c == ':':
		end := strings.IndexByte(p.s[p.pos+1:], ':')
		if end < 0 {
			return item{}, p.fail("unterminated byte sequence")
		}
		p.pos += end + 2
		return item{}, nil
	case c == '?':
		p.pos++
		if b := p.peek(); b != '0' && b != '1' {
			return item{}, p.fail("expected a boolean")
		}
		p.pos++
		return item{}, nil
	default:
		return item{}, p.fail("expected an item")
	}
}

// number parses an sf-integer (at most 15 digits) or skips an
// sf-decimal.
func (p *parser) number() (item, error) {
	start := p.pos
	if p.peek() == '-' {
		p.pos++
	}
	digits := p.pos
	for !p.eof() && isDigit(p.s[p.pos]) {
		p.pos++
	}
	n := p.pos - digits
	if n == 0 || n > 15 {
		return item{}, p.fail("bad integer")
	}
	if p.peek() == '.' {
		p.pos++
		frac := p.pos
		for !p.eof() && isDigit(p.s[p.pos]) {
			p.pos++
		}
		if f := p.pos - frac; f == 0 || f > 3 || n > 12 {
			return item{}, p.fail("bad decimal")
		}
		return item{}, nil
	}
	v, err := strconv.Atoi(p.s[start:p.pos])
	if err != nil {
		return item{}, p.fail("bad integer")
	}
	return item{kind: kindInt, num: v}, nil
}

// str parses an sf-string: printable ASCII, with '\' escaping '"' and
// '\' only.
func (p *parser) str() (string, error) {
	p.pos++ // opening quote
	var b strings.Builder
	for !p.eof() {
		c := p.s[p.pos]
		p.pos++
		switch {
		case c == '\\':
			if p.eof() || (p.s[p.pos] != '"' && p.s[p.pos] != '\\') {
				return "", p.fail("bad escape")
			}
			b.WriteByte(p.s[p.pos])
			p.pos++
		case c == '"':
			return b.String(), nil
		case c < 0x20 || c > 0x7e:
			return "", p.fail("non-printable byte in string")
		default:
			b.WriteByte(c)
		}
	}
	return "", p.fail("unterminated string")
}
