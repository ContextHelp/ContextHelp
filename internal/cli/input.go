package cli

import (
	"fmt"
	"io"
	"os"

	"strings"

	"github.com/atotto/clipboard"
)

// GetInput retrieves content from arguments, stdin, or clipboard.
// order of precedence:
// 1. args joined by spaces if present
// 2. stdin if it's NOT a TTY (piped)
// 3. clipboard if stdin IS a TTY and clipboard is not empty
func GetInput(args []string) (string, string, error) {
	// 1. Check arguments
	if len(args) > 0 {
		content := strings.Join(args, " ")
		if content != "" {
			return content, "argument", nil
		}
	}

	// 2. Check stdin (only if piped)
	stat, _ := os.Stdin.Stat()
	if (stat.Mode() & os.ModeCharDevice) == 0 {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", "", fmt.Errorf("failed to read stdin: %w", err)
		}
		if content := strings.TrimSpace(string(data)); content != "" {
			return content, "stdin", nil
		}
	}

	// 3. Check clipboard (only if TTY and not disabled)
	if clipboard.Unsupported || os.Getenv("CTXT_NO_CLIPBOARD") != "" {
		return "", "", fmt.Errorf("no input provided and clipboard is unsupported on this platform")
	}

	content, err := clipboard.ReadAll()
	if err == nil && content != "" {
		return content, "clipboard", nil
	}

	return "", "", fmt.Errorf("no input provided (use argument, stdin, or clipboard)")
}
