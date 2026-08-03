package value

// DocumentRevisionStatus describes immutable revision processing progress.
type DocumentRevisionStatus string

const (
	DocumentRevisionPending DocumentRevisionStatus = "pending"
	DocumentRevisionParsing DocumentRevisionStatus = "parsing"
	DocumentRevisionReady   DocumentRevisionStatus = "ready"
	DocumentRevisionFailed  DocumentRevisionStatus = "failed"
)

// DocumentRevisionReason describes why a complete revision was created.
type DocumentRevisionReason string

const (
	DocumentRevisionReasonIngest  DocumentRevisionReason = "ingest"
	DocumentRevisionReasonReplace DocumentRevisionReason = "replace"
	DocumentRevisionReasonReparse DocumentRevisionReason = "reparse"
	DocumentRevisionReasonCrawl   DocumentRevisionReason = "crawl"
	DocumentRevisionReasonEdit    DocumentRevisionReason = "edit"
)
