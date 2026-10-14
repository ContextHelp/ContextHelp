package cmd

var importSafariCmd = newBookmarkImportCmd(bookmarkBrowser{name: "safari", label: "Safari", longName: "Safari"})

func init() {
	importCmd.AddCommand(importSafariCmd)
}
