package taxonomy

// Subtypes for TypeText.
const (
	SubtypeTextShort = "short" // raw content < 500 chars
	SubtypeTextLong  = "long"  // raw content >= 500 chars
)

// Subtypes for TypeDocument.
const (
	SubtypeDocumentPDF      = "pdf"
	SubtypeDocumentMarkdown = "markdown"
	SubtypeDocumentCode     = "code"
	SubtypeDocumentOffice   = "office"
	SubtypeDocumentEPUB     = "epub"
	SubtypeDocumentHTML     = "html"
)

// Subtypes for TypeText used in email pipelines (set by email_filter/enqueuer).
const (
	SubtypeEmailBilling    = "billing"
	SubtypeEmailNewsletter = "newsletter"
	SubtypeEmailAttachment = "email-attachment"
	SubtypeEmail           = "email"
)
