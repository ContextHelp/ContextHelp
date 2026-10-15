package cmd

var importChromeCmd = newBookmarkImportCmd(bookmarkBrowser{name: "chrome", label: "Chrome", longName: "Chrome"})

func init() {
	importCmd.AddCommand(importChromeCmd)
}
