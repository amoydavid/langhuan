package value

type DocumentStatus string

const (
	DocumentStatusPending          DocumentStatus = "pending"
	DocumentStatusProcessing       DocumentStatus = "processing"
	DocumentStatusReady            DocumentStatus = "ready"
	DocumentStatusParsingSubmitted DocumentStatus = "parsing_submitted"
	DocumentStatusParsing          DocumentStatus = "parsing"
	DocumentStatusParsed           DocumentStatus = "parsed"
	DocumentStatusIndexing         DocumentStatus = "indexing"
	DocumentStatusCompleted        DocumentStatus = "completed"
	DocumentStatusFailed           DocumentStatus = "failed"
	DocumentStatusDeleting         DocumentStatus = "deleting"
	DocumentStatusDeleted          DocumentStatus = "deleted"
)
