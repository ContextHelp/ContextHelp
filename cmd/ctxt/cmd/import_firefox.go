package cmd

var importFirefoxCmd = newBookmarkImportCmd(bookmarkBrowser{name: "firefox", label: "Firefox", longName: "Mozilla Firefox"})

func init() {
	importCmd.AddCommand(importFirefoxCmd)
}
