package chromium

import (
	"bytes"
	"encoding/binary"
	"unicode/utf16"
)

// snssBuilder writes synthetic SNSS session files for tests. It mirrors
// the on-disk layout Chromium's CommandStorageBackend produces: an 8-byte
// header followed by <uint16 size><uint8 id><payload> records, where size
// counts the id byte plus the payload.
type snssBuilder struct {
	buf bytes.Buffer
}

func newSNSS(version int32) *snssBuilder {
	b := &snssBuilder{}
	b.buf.WriteString("SNSS")
	b.le(version)
	return b
}

func (b *snssBuilder) le(v any) {
	if err := binary.Write(&b.buf, binary.LittleEndian, v); err != nil {
		panic(err)
	}
}

func (b *snssBuilder) bytes() []byte { return b.buf.Bytes() }

func (b *snssBuilder) command(id uint8, payload []byte) *snssBuilder {
	b.le(uint16(len(payload) + 1))
	b.buf.WriteByte(id)
	b.buf.Write(payload)
	return b
}

func pack(vals ...any) []byte {
	var out bytes.Buffer
	for _, v := range vals {
		if err := binary.Write(&out, binary.LittleEndian, v); err != nil {
			panic(err)
		}
	}
	return out.Bytes()
}

func (b *snssBuilder) tabWindow(window, tab int32) *snssBuilder {
	return b.command(cmdSetTabWindow, pack(window, tab))
}

func (b *snssBuilder) tabIndex(tab, index int32) *snssBuilder {
	return b.command(cmdSetTabIndexInWindow, pack(tab, index))
}

func (b *snssBuilder) selectedNav(tab, index int32) *snssBuilder {
	return b.command(cmdSetSelectedNavigationIndex, pack(tab, index))
}

func (b *snssBuilder) selectedTab(window, index int32) *snssBuilder {
	return b.command(cmdSetSelectedTabInIndex, pack(window, index))
}

func (b *snssBuilder) pinned(tab int32, on bool) *snssBuilder {
	var flag uint8
	if on {
		flag = 1
	}
	// bool followed by three bytes of struct padding.
	return b.command(cmdSetPinnedState, pack(tab, flag, [3]byte{}))
}

func (b *snssBuilder) tabClosed(tab int32) *snssBuilder {
	// id, four bytes of alignment padding, int64 close time.
	return b.command(cmdTabClosed, pack(tab, int32(0), int64(1)))
}

func (b *snssBuilder) windowClosed(window int32) *snssBuilder {
	return b.command(cmdWindowClosed, pack(window, int32(0), int64(1)))
}

func (b *snssBuilder) activeWindow(window int32) *snssBuilder {
	return b.command(cmdSetActiveWindow, pack(window))
}

func (b *snssBuilder) pruned(tab, index, count int32) *snssBuilder {
	return b.command(cmdTabNavigationPathPruned, pack(tab, index, count))
}

func (b *snssBuilder) prunedFromBack(tab, index int32) *snssBuilder {
	return b.command(cmdTabNavigationPathPrunedFromBack, pack(tab, index))
}

func (b *snssBuilder) marker() *snssBuilder {
	return b.command(cmdInitialStateMarker, nil)
}

// nav writes an UpdateTabNavigation pickle: uint32 payload size, tab id,
// navigation index, URL (std::string), title (std::u16string), then the
// trailing serialized-navigation fields Chromium appends, which the
// reader must ignore.
func (b *snssBuilder) nav(tab, index int32, url, title string) *snssBuilder {
	var body bytes.Buffer
	w := func(v any) {
		if err := binary.Write(&body, binary.LittleEndian, v); err != nil {
			panic(err)
		}
	}
	w(tab)
	w(index)
	w(uint32(len(url)))
	body.WriteString(url)
	body.Write(make([]byte, pad4(len(url))))
	units := utf16.Encode([]rune(title))
	w(uint32(len(units)))
	w(units)
	body.Write(make([]byte, pad4(len(units)*2)))
	// Trailing fields: empty page state string, transition type, type mask.
	w(uint32(0))
	w(int32(0))
	w(int32(0))
	payload := append(pack(uint32(body.Len())), body.Bytes()...)
	return b.command(cmdUpdateTabNavigation, payload)
}

// tab is shorthand for a single-navigation tab placed in a window.
func (b *snssBuilder) tab(window, tab, index int32, url, title string) *snssBuilder {
	return b.tabWindow(window, tab).
		tabIndex(tab, index).
		nav(tab, 0, url, title).
		selectedNav(tab, 0)
}
