package builtins

func init() {
	MustRegister("dropbox.sync", Def{
		Description: "Dropbox folder sync pipeline (cursor-based incremental)",
		Steps:       []string{"dropbox_fetcher", "dropbox_enqueuer"},
	})
}
