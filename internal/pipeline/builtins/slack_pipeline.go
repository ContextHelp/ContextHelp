package builtins

func init() {
	MustRegister("import.slack", Def{
		Description: "Slack workspace export import pipeline",
		Steps:       []string{"slack_parser"},
	})
}
