package builtins

func init() {
	MustRegister("import.discord", Def{
		Description: "Discord message export import pipeline",
		Steps:       []string{"discord_parser"},
	})
}
