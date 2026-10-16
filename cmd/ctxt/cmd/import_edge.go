package cmd

var importEdgeCmd = newBookmarkImportCmd(bookmarkBrowser{name: "edge", label: "Edge", longName: "Microsoft Edge"})

func init() {
	importCmd.AddCommand(importEdgeCmd)
}
