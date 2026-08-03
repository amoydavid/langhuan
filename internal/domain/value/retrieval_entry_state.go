package value

// RetrievalEntryState describes a rebuildable search projection row.
type RetrievalEntryState string

const (
	RetrievalEntryStaging   RetrievalEntryState = "staging"
	RetrievalEntryPublished RetrievalEntryState = "published"
	RetrievalEntryRetired   RetrievalEntryState = "retired"
	RetrievalEntryFailed    RetrievalEntryState = "failed"
)
