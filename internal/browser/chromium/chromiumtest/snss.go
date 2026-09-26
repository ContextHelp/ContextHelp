// Package chromiumtest builds synthetic Chromium on-disk state for tests:
// SNSS session files and user data directories with a Local State file
// and per-profile Sessions folders. It lets tests outside package
// chromium exercise profile resolution and tab reading without a real
// browser install.
package chromiumtest

import (
	"bytes"
	"encoding/binary"
	"unicode/utf16"
)

// SNSS format versions.
const (
	VersionPlain      int32 = 1
	VersionWithMarker int32 = 3
)

// Session command ids, mirroring package chromium (and Chromium's
// session_service_commands.cc). Package chromium's tests assert the two
// lists agree.
const (
	CmdSetTabWindow                    uint8 = 0
	CmdSetTabIndexInWindow             uint8 = 2
	CmdTabNavigationPathPrunedFromBack uint8 = 5
	CmdUpdateTabNavigation             uint8 = 6
	CmdSetSelectedNavigationIndex      uint8 = 7
	CmdSetSelectedTabInIndex           uint8 = 8
	CmdSetPinnedState                  uint8 = 12
	CmdTabClosed                       uint8 = 16
	CmdWindowClosed                    uint8 = 17
	CmdSetActiveWindow                 uint8 = 20
	CmdTabNavigationPathPruned         uint8 = 24
	CmdInitialStateMarker              uint8 = 255
)

// SNSS writes a synthetic SNSS session file. It mirrors the on-disk
// layout Chromium's CommandStorageBackend produces: an 8-byte header
// followed by <uint16 size><uint8 id><payload> records, where size counts
// the id byte plus the payload. Methods chain; Bytes returns the file.
type SNSS struct {
	buf bytes.Buffer
}

// NewSNSS starts a session file with the given format version.
func NewSNSS(version int32) *SNSS {
	b := &SNSS{}
	b.buf.WriteString("SNSS")
	b.le(version)
	return b
}

func (b *SNSS) le(v any) {
	if err := binary.Write(&b.buf, binary.LittleEndian, v); err != nil {
		panic(err)
	}
}

// Bytes returns the encoded file.
func (b *SNSS) Bytes() []byte { return b.buf.Bytes() }

// Command appends a raw command record.
func (b *SNSS) Command(id uint8, payload []byte) *SNSS {
	b.le(uint16(len(payload) + 1)) // #nosec G115 -- test payloads are tiny
	b.buf.WriteByte(id)
	b.buf.Write(payload)
	return b
}

// Pack encodes vals little-endian, back to back.
func Pack(vals ...any) []byte {
	var out bytes.Buffer
	for _, v := range vals {
		if err := binary.Write(&out, binary.LittleEndian, v); err != nil {
			panic(err)
		}
	}
	return out.Bytes()
}

// TabWindow places tab in window.
func (b *SNSS) TabWindow(window, tab int32) *SNSS {
	return b.Command(CmdSetTabWindow, Pack(window, tab))
}

// TabIndex sets tab's position within its window.
func (b *SNSS) TabIndex(tab, index int32) *SNSS {
	return b.Command(CmdSetTabIndexInWindow, Pack(tab, index))
}

// SelectedNav selects tab's navigation entry at index.
func (b *SNSS) SelectedNav(tab, index int32) *SNSS {
	return b.Command(CmdSetSelectedNavigationIndex, Pack(tab, index))
}

// SelectedTab selects the tab at index in window.
func (b *SNSS) SelectedTab(window, index int32) *SNSS {
	return b.Command(CmdSetSelectedTabInIndex, Pack(window, index))
}

// Pinned sets tab's pinned state.
func (b *SNSS) Pinned(tab int32, on bool) *SNSS {
	var flag uint8
	if on {
		flag = 1
	}
	// bool followed by three bytes of struct padding.
	return b.Command(CmdSetPinnedState, Pack(tab, flag, [3]byte{}))
}

// TabClosed closes tab.
func (b *SNSS) TabClosed(tab int32) *SNSS {
	// id, four bytes of alignment padding, int64 close time.
	return b.Command(CmdTabClosed, Pack(tab, int32(0), int64(1)))
}

// WindowClosed closes window.
func (b *SNSS) WindowClosed(window int32) *SNSS {
	return b.Command(CmdWindowClosed, Pack(window, int32(0), int64(1)))
}

// ActiveWindow marks window active.
func (b *SNSS) ActiveWindow(window int32) *SNSS {
	return b.Command(CmdSetActiveWindow, Pack(window))
}

// Pruned drops count navigation entries of tab starting at index.
func (b *SNSS) Pruned(tab, index, count int32) *SNSS {
	return b.Command(CmdTabNavigationPathPruned, Pack(tab, index, count))
}

// PrunedFromBack drops tab's navigation entries from index onwards.
func (b *SNSS) PrunedFromBack(tab, index int32) *SNSS {
	return b.Command(CmdTabNavigationPathPrunedFromBack, Pack(tab, index))
}

// Marker appends the initial-state marker a version-3 file needs.
func (b *SNSS) Marker() *SNSS {
	return b.Command(CmdInitialStateMarker, nil)
}

// Nav writes an UpdateTabNavigation pickle: uint32 payload size, tab id,
// navigation index, URL (std::string), title (std::u16string), then the
// trailing serialized-navigation fields Chromium appends, which the
// reader must ignore.
func (b *SNSS) Nav(tab, index int32, url, title string) *SNSS {
	var body bytes.Buffer
	w := func(v any) {
		if err := binary.Write(&body, binary.LittleEndian, v); err != nil {
			panic(err)
		}
	}
	w(tab)
	w(index)
	w(uint32(len(url))) // #nosec G115 -- test strings are tiny
	body.WriteString(url)
	body.Write(make([]byte, pad4(len(url))))
	units := utf16.Encode([]rune(title))
	w(uint32(len(units))) // #nosec G115 -- test strings are tiny
	w(units)
	body.Write(make([]byte, pad4(len(units)*2)))
	// Trailing fields: empty page state string, transition type, type mask.
	w(uint32(0))
	w(int32(0))
	w(int32(0))
	payload := append(Pack(uint32(body.Len())), body.Bytes()...) // #nosec G115 -- test payloads are tiny
	return b.Command(CmdUpdateTabNavigation, payload)
}

// Tab is shorthand for a single-navigation tab placed in a window.
func (b *SNSS) Tab(window, tab, index int32, url, title string) *SNSS {
	return b.TabWindow(window, tab).
		TabIndex(tab, index).
		Nav(tab, 0, url, title).
		SelectedNav(tab, 0)
}

func pad4(n int) int { return (4 - n%4) % 4 }
