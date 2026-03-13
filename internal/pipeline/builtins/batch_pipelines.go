package builtins

func init() {
	MustRegister("batch.jsonl", Def{
		Description: "JSONL batch import pipeline",
		Extensions:  []string{".jsonl", ".ndjson"},
		Steps:       []string{"jsonl_parser", "record_validator", "batch_enqueuer"},
	})
	MustRegister("batch.csv", Def{
		Description: "CSV batch import pipeline",
		Extensions:  []string{".csv"},
		Steps:       []string{"csv_parser", "record_validator", "batch_enqueuer"},
	})
	MustRegister("batch.tsv", Def{
		Description: "TSV batch import pipeline",
		Extensions:  []string{".tsv"},
		Steps:       []string{"csv_parser", "record_validator", "batch_enqueuer"},
	})
}
