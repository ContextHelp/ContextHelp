package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestShellCmd_Registration(t *testing.T) {
	assert.Equal(t, "shell", shellCmd.Use)
	assert.NotEmpty(t, shellCmd.Short)
	assert.NotNil(t, shellCmd.RunE)
}
