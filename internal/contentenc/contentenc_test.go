package contentenc

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
)

func TestEncodeTextStaysPlain(t *testing.T) {
	for _, s := range []string{"", "plain", "héllo ✓", "line\r\n\x00nul"} {
		c, enc := Encode(s)
		require.Equal(t, s, c)
		require.Empty(t, enc, "valid UTF-8 must carry no content_encoding: %q", s)
	}
}

func TestEncodeInvalidUTF8RoundTrips(t *testing.T) {
	bin := []byte{0x89, 'P', 'N', 'G', 0xff, 0xfe, 0x00, 0x80, 0xc3, 0x28}
	for name, f := range map[string]func() (string, string){
		"string": func() (string, string) { return Encode(string(bin)) },
		"bytes":  func() (string, string) { return EncodeBytes(bin) },
	} {
		c, enc := f()
		require.Equal(t, Base64, enc, name)
		got, err := Decode(c, enc)
		require.NoError(t, err, name)
		require.Equal(t, bin, []byte(got), name)
	}
}

func TestDecodeErrors(t *testing.T) {
	_, err := Decode("not base64!!", Base64)
	require.True(t, errors.Is(err, ErrInvalidBase64))
	_, err = Decode("x", "gzip")
	require.True(t, errors.Is(err, ErrUnknown))
	got, err := Decode("hi", "")
	require.NoError(t, err)
	require.Equal(t, "hi", got)
}

// A caller-supplied encoding is echoed back truncated, not whole.
func TestDecodeUnknownEncodingEchoIsBounded(t *testing.T) {
	long := strings.Repeat("é", 500)
	_, err := Decode("x", long)
	require.ErrorIs(t, err, ErrUnknown)
	require.Less(t, len(err.Error()), 150, err.Error())
	require.True(t, utf8.ValidString(err.Error()), "cut on a rune boundary")
	_, err = Decode("x", "rot13")
	require.Contains(t, err.Error(), `"rot13"`)
}
