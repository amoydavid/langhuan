package value

// IndexGenerationStatus describes a double-buffer index generation lifecycle.
type IndexGenerationStatus string

const (
	IndexGenerationBuilding IndexGenerationStatus = "building"
	IndexGenerationReady    IndexGenerationStatus = "ready"
	IndexGenerationStale    IndexGenerationStatus = "stale"
	IndexGenerationFailed   IndexGenerationStatus = "failed"
	IndexGenerationRetired  IndexGenerationStatus = "retired"
)

// ManualEditDisposition controls activation when rechunking would archive edits.
type ManualEditDisposition string

const (
	ManualEditNotApplicable    ManualEditDisposition = "not_applicable"
	ManualEditPending          ManualEditDisposition = "pending"
	ManualEditArchiveConfirmed ManualEditDisposition = "archive_confirmed"
)
