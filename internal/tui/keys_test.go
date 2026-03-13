package tui_test

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/tui"
	"github.com/stretchr/testify/assert"
)

func TestDefaultKeyMapNonEmpty(t *testing.T) {
	km := tui.DefaultKeyMap()
	assert.NotEmpty(t, km.Search.Keys())
	assert.NotEmpty(t, km.NextPane.Keys())
	assert.NotEmpty(t, km.PrevPane.Keys())
	assert.NotEmpty(t, km.Capture.Keys())
	assert.NotEmpty(t, km.Quit.Keys())
	assert.NotEmpty(t, km.Help.Keys())
}

func TestKeyMapImplementsHelpKeyMap(t *testing.T) {
	km := tui.DefaultKeyMap()
	short := km.ShortHelp()
	assert.NotEmpty(t, short)
	full := km.FullHelp()
	assert.NotEmpty(t, full)
}
