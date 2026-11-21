package cli

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

// pipeStdin makes os.Stdin a pipe holding data for the test.
func pipeStdin(t *testing.T, data []byte) {
	t.Helper()
	r, w, err := os.Pipe()
	require.NoError(t, err)
	_, err = w.Write(data)
	require.NoError(t, err)
	require.NoError(t, w.Close())
	old := os.Stdin
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = old; r.Close() })
}

func TestGetInputStdinTrimsText(t *testing.T) {
	pipeStdin(t, []byte("  héllo\n\n"))
	got, src, err := GetInput(nil)
	require.NoError(t, err)
	require.Equal(t, SourceStdin, src)
	require.Equal(t, "héllo", got)
}

// Binary stdin (a piped PNG) is content exactly as it came: its leading
// and trailing whitespace bytes are not padding.
func TestGetInputStdinKeepsBinaryExact(t *testing.T) {
	bin := []byte{'\n', 0x89, 'P', 'N', 'G', 0xff, 0x00, 0x80, ' ', '\n'}
	pipeStdin(t, bin)
	got, src, err := GetInput(nil)
	require.NoError(t, err)
	require.Equal(t, SourceStdin, src)
	require.Equal(t, bin, []byte(got))
}
