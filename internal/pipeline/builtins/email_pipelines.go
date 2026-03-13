package builtins

func init() {
	MustRegister("email.newsletter", Def{
		Description: "Email newsletter ingestion pipeline: parse, filter, enqueue",
		Extensions:  []string{".eml"},
		Steps:       []string{"email_parser", "email_filter", "email_enqueuer"},
	})
	MustRegister("email.billing", Def{
		Description: "Email billing/invoice ingestion pipeline: parse, filter, enqueue",
		Steps:       []string{"email_parser", "email_filter", "email_enqueuer"},
	})
	MustRegister("email.general", Def{
		Description: "General email ingestion pipeline: parse, filter, enqueue",
		Extensions:  []string{".mbox"},
		Steps:       []string{"email_parser", "email_filter", "email_enqueuer"},
	})
}
