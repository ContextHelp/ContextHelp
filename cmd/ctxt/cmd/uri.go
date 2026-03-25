package cmd

import (
	"fmt"
	"os"
	"runtime"

	"github.com/spf13/cobra"
	"hop.top/hdl"
	"hop.top/hdl/generate"
)

// ctxtBundleID is the macOS bundle identifier for the ctxt application.
const ctxtBundleID = "com.ideacrafterslabs.ctxt"

var uriCmd = &cobra.Command{
	Use:   "uri",
	Short: "Manage ctxt:// URI scheme registration",
}

var uriRegisterCmd = &cobra.Command{
	Use:   "register",
	Short: "Register ctxt:// URI scheme with the OS",
	Long: `Register the ctxt:// URI scheme with the operating system.

After registration, clicking a ctxt:// link will open ctxt automatically.

On macOS, registers via LSSetDefaultHandlerForURLScheme using the bundle ID.
On Linux, writes a .desktop file via xdg-mime.
On Windows, writes a registry entry under HKCU.`,
	RunE: runURIRegister,
}

var uriSnippetCmd = &cobra.Command{
	Use:   "snippet",
	Short: "Print a static registration snippet for bundled packaging",
	Long: `Print a platform-specific configuration snippet for registering ctxt://.

Useful when runtime registration is not possible (e.g. app bundles, iOS).

Examples:
  ctxt uri snippet --platform macos
  ctxt uri snippet --platform linux
  ctxt uri snippet --platform windows`,
	RunE: runURISnippet,
}

func init() {
	rootCmd.AddCommand(uriCmd)
	uriCmd.AddCommand(uriRegisterCmd)
	uriCmd.AddCommand(uriSnippetCmd)

	uriSnippetCmd.Flags().String("platform", runtime.GOOS, "target platform (macos|ios|linux|windows)")
}

func runURIRegister(cmd *cobra.Command, _ []string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable: %w", err)
	}

	// macOS uses bundle ID; other platforms use binary path.
	appID := exe
	if runtime.GOOS == "darwin" {
		appID = ctxtBundleID
	}

	if err := hdl.Register("ctxt", appID); err != nil {
		return fmt.Errorf("register ctxt:// scheme: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "ctxt:// registered (app: %s)\n", appID)
	return nil
}

func runURISnippet(cmd *cobra.Command, _ []string) error {
	platform, _ := cmd.Flags().GetString("platform")

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable: %w", err)
	}

	snippet, err := generate.Snippet(platform, "ctxt", exe)
	if err != nil {
		return fmt.Errorf("generate snippet: %w", err)
	}

	fmt.Fprintln(cmd.OutOrStdout(), snippet)
	return nil
}
