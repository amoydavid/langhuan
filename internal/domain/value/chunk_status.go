package value

// ChunkStrategy selects the deterministic chunk construction contract.
type ChunkStrategy string

const (
	ChunkStrategyStandard ChunkStrategy = "standard"
	ChunkStrategyFAQ      ChunkStrategy = "faq"
)

// ChunkSetStatus describes a complete chunk set build.
type ChunkSetStatus string

const (
	ChunkSetBuilding ChunkSetStatus = "building"
	ChunkSetReady    ChunkSetStatus = "ready"
	ChunkSetFailed   ChunkSetStatus = "failed"
	ChunkSetArchived ChunkSetStatus = "archived"
)

// ChunkRevisionStatus describes indexing progress for one revision.
type ChunkRevisionStatus string

const (
	ChunkRevisionPending  ChunkRevisionStatus = "pending"
	ChunkRevisionIndexing ChunkRevisionStatus = "indexing"
	ChunkRevisionReady    ChunkRevisionStatus = "ready"
	ChunkRevisionFailed   ChunkRevisionStatus = "failed"
)

// ChunkEditSource records whether a revision was produced by the system or user.
type ChunkEditSource string

const (
	ChunkEditSourceSystem ChunkEditSource = "system"
	ChunkEditSourceUser   ChunkEditSource = "user"
)
