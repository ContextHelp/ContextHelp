package chromium

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"unicode/utf16"
)

// SNSS is the append-only command log Chromium writes to
// <profile>/Sessions/Session_<timestamp>. Layout, all little-endian:
//
//	"SNSS" int32(version)
//	{ uint16(size) uint8(command id) payload[size-1] }...
//
// Most payloads are raw C structs (with natural alignment padding);
// UpdateTabNavigation is a base::Pickle. Version 3 files carry an
// initial-state marker command once the rebuilt snapshot is fully
// written; Chromium discards a v3 file without one. Versions 2 and 4 are
// the encrypted variants and are not supported.

const (
	snssMagic = "SNSS"

	snssVersionPlain      int32 = 1
	snssVersionWithMarker int32 = 3
)

// Session command ids, from Chromium's session_service_commands.cc.
// Only ids relevant to tab replay are listed; every other id is skipped.
// SetActiveWindow is recognized but unused: Tab.Active is per window.
const (
	cmdSetTabWindow                    uint8 = 0
	cmdSetTabIndexInWindow             uint8 = 2
	cmdTabNavigationPathPrunedFromBack uint8 = 5
	cmdUpdateTabNavigation             uint8 = 6
	cmdSetSelectedNavigationIndex      uint8 = 7
	cmdSetSelectedTabInIndex           uint8 = 8
	cmdSetPinnedState                  uint8 = 12
	cmdTabClosed                       uint8 = 16
	cmdWindowClosed                    uint8 = 17
	cmdSetActiveWindow                 uint8 = 20
	cmdTabNavigationPathPruned         uint8 = 24
	cmdInitialStateMarker              uint8 = 255
)

var (
	// ErrNoSessionFile reports a profile directory with no
	// Sessions/Session_* file.
	ErrNoSessionFile = errors.New("chromium: no session file")

	// ErrBadHeader reports input that is not a readable SNSS file: wrong
	// magic, a short header, or an unsupported (e.g. encrypted) version.
	// The concrete error is a *HeaderError.
	ErrBadHeader = errors.New("chromium: bad SNSS header")

	// ErrIncompleteSession reports a version-3 file whose initial-state
	// marker is missing, i.e. a snapshot Chromium had not finished
	// writing. Chromium ignores such files and so does OpenTabs.
	ErrIncompleteSession = errors.New("chromium: session file has no initial-state marker")
)

// HeaderError describes an SNSS header that failed validation.
type HeaderError struct {
	Magic   string // first bytes read, up to four
	Version int32  // zero when the header was too short to hold one
	Short   bool   // fewer than eight header bytes were available
}

func (e *HeaderError) Error() string {
	switch {
	case e.Short:
		return fmt.Sprintf("%v: short header (%d magic bytes)", ErrBadHeader, len(e.Magic))
	case e.Magic != snssMagic:
		return fmt.Sprintf("%v: magic %q", ErrBadHeader, e.Magic)
	default:
		return fmt.Sprintf("%v: unsupported version %d", ErrBadHeader, e.Version)
	}
}

// Is makes errors.Is(err, ErrBadHeader) match a *HeaderError.
func (e *HeaderError) Is(target error) bool { return target == ErrBadHeader }

// snssCommand is one decoded record.
type snssCommand struct {
	id      uint8
	payload []byte
}

// snssReader yields commands from an SNSS stream.
type snssReader struct {
	r       *bufio.Reader
	version int32
}

func newSNSSReader(r io.Reader) (*snssReader, error) {
	br := bufio.NewReader(r)
	var hdr [8]byte
	n, err := io.ReadFull(br, hdr[:])
	if err != nil {
		magic := hdr[:min(n, 4)]
		return nil, &HeaderError{Magic: string(magic), Short: true}
	}
	magic := string(hdr[:4])
	version := int32(binary.LittleEndian.Uint32(hdr[4:])) //nolint:gosec // reinterpreting the on-disk int32
	if magic != snssMagic || (version != snssVersionPlain && version != snssVersionWithMarker) {
		return nil, &HeaderError{Magic: magic, Version: version}
	}
	return &snssReader{r: br, version: version}, nil
}

// next returns the next complete command. It returns io.EOF at the end
// of the stream and also when the final record is truncated: the file
// is appended to live, so a partial trailing record is normal and is
// dropped rather than reported.
func (s *snssReader) next() (snssCommand, error) {
	var sizeBuf [2]byte
	if _, err := io.ReadFull(s.r, sizeBuf[:]); err != nil {
		return snssCommand{}, eofOrErr(err)
	}
	size := binary.LittleEndian.Uint16(sizeBuf[:])
	if size == 0 {
		// Chromium never writes an empty record; treat it as the end of
		// the valid log.
		return snssCommand{}, io.EOF
	}
	buf := make([]byte, size)
	if _, err := io.ReadFull(s.r, buf); err != nil {
		return snssCommand{}, eofOrErr(err)
	}
	return snssCommand{id: buf[0], payload: buf[1:]}, nil
}

func eofOrErr(err error) error {
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return io.EOF
	}
	return err
}

// payloadReader decodes little-endian fields from a command payload,
// latching the first short read so callers check once at the end.
type payloadReader struct {
	b   []byte
	bad bool
}

func (p *payloadReader) take(n int) []byte {
	if p.bad || n < 0 || n > len(p.b) {
		p.bad = true
		return nil
	}
	out := p.b[:n]
	p.b = p.b[n:]
	return out
}

func (p *payloadReader) int32() int32 {
	b := p.take(4)
	if b == nil {
		return 0
	}
	return int32(binary.LittleEndian.Uint32(b)) //nolint:gosec // reinterpreting the on-disk int32
}

func (p *payloadReader) uint8() uint8 {
	b := p.take(1)
	if b == nil {
		return 0
	}
	return b[0]
}

// pickleString reads a base::Pickle std::string: uint32 byte length,
// bytes, padding to a four-byte boundary.
func (p *payloadReader) pickleString() string {
	n := p.int32()
	if n < 0 {
		p.bad = true
		return ""
	}
	b := p.take(int(n))
	p.take(pad4(int(n)))
	return string(b)
}

// pickleString16 reads a base::Pickle std::u16string: uint32 length in
// UTF-16 code units, the units, padding to a four-byte boundary.
func (p *payloadReader) pickleString16() string {
	n := p.int32()
	if n < 0 || int(n) > len(p.b)/2 {
		p.bad = true
		return ""
	}
	b := p.take(int(n) * 2)
	p.take(pad4(int(n) * 2))
	if p.bad {
		return ""
	}
	units := make([]uint16, n)
	for i := range units {
		units[i] = binary.LittleEndian.Uint16(b[2*i:])
	}
	return string(utf16.Decode(units))
}

func pad4(n int) int { return (4 - n%4) % 4 }
