package pipeline

import (
	"fmt"
	id "github.com/dajee/langhuan/internal/domain/id"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

// CurrentStandardChunkerVersion is the current deterministic standard chunking contract.
const CurrentStandardChunkerVersion = value.StandardChunkerVersion

// Chunker deterministically splits parsed document structures into retrieval chunks.
type Chunker struct{}

// NewChunker creates a stateless structured chunker.
func NewChunker() Chunker { return Chunker{} }

// ChunkInput carries the complete tenant and immutable revision lineage.
type ChunkInput struct {
	WorkspaceID        uuid.UUID
	KnowledgeBaseID    uuid.UUID
	DocumentID         uuid.UUID
	DocumentRevisionID uuid.UUID
	ChunkSetID         uuid.UUID
	Kind               value.DocumentKind
	Title              string
	Markdown           string
	Manifest           model.ParseManifest
}

type chunkDraft struct {
	content     string
	headingPath []string
	anchor      value.SourceAnchor
	metadata    map[string]any
}

type blockRange struct {
	block     model.ParsedBlock
	runeStart int
	runeEnd   int
}

// Chunk validates the parse result and produces Chunks with their first system revisions.
func (Chunker) Chunk(input ChunkInput, config value.ChunkingConfig) ([]*model.Chunk, []*model.ChunkRevision, error) {
	if input.WorkspaceID == uuid.Nil || input.KnowledgeBaseID == uuid.Nil || input.DocumentID == uuid.Nil ||
		input.DocumentRevisionID == uuid.Nil || input.ChunkSetID == uuid.Nil {
		return nil, nil, fmt.Errorf("%w: document chunk lineage is required", domainerrors.ErrValidation)
	}
	if input.Kind == value.DocumentKindFAQ {
		return nil, nil, fmt.Errorf("%w: standard chunker does not accept FAQ documents", domainerrors.ErrValidation)
	}
	if input.Kind != value.DocumentKindFile && input.Kind != value.DocumentKindWeb {
		return nil, nil, fmt.Errorf("%w: unsupported document kind %q", domainerrors.ErrValidation, input.Kind)
	}
	config = config.Normalize()
	if err := config.Validate(); err != nil {
		return nil, nil, err
	}
	if err := input.Manifest.Validate(input.Markdown); err != nil {
		return nil, nil, fmt.Errorf("invalid parse manifest: %w", err)
	}

	workingConfig := config
	if config.EnableParentChild {
		workingConfig.ChunkSize = config.ChildChunkSize
		workingConfig.ChunkOverlap = config.ChildChunkSize / 5
	}
	var drafts []chunkDraft
	var lastErr error
	for _, strategy := range selectChunkingStrategies(input.Manifest, config) {
		drafts, lastErr = splitChunkDrafts(input.Markdown, input.Manifest.Blocks, workingConfig, strategy)
		if lastErr == nil {
			break
		}
	}
	if lastErr != nil {
		return nil, nil, lastErr
	}

	if !config.EnableParentChild {
		return materializeChunkDrafts(input, drafts, value.ChunkRoleFlat, nil)
	}
	return materializeParentChildDrafts(input, drafts, config)
}

func splitChunkDrafts(markdown string, blocks []model.ParsedBlock, config value.ChunkingConfig, strategy value.ChunkingStrategy) ([]chunkDraft, error) {
	drafts := make([]chunkDraft, 0)
	ordinary := make([]model.ParsedBlock, 0)
	flushOrdinary := func() error {
		if len(ordinary) == 0 {
			return nil
		}
		created, err := splitOrdinary(markdown, ordinary, config)
		if err != nil {
			return err
		}
		drafts = append(drafts, created...)
		ordinary = ordinary[:0]
		return nil
	}

	for index := 0; index < len(blocks); {
		block := blocks[index]
		if block.Kind == model.BlockKindTableHeader {
			if err := flushOrdinary(); err != nil {
				return nil, err
			}
			tableID, _ := block.Metadata["table_id"].(string)
			rows := make([]model.ParsedBlock, 0)
			cursor := index + 1
			for cursor < len(blocks) && blocks[cursor].Kind == model.BlockKindTableRow {
				rowID, _ := blocks[cursor].Metadata["table_id"].(string)
				if rowID != tableID {
					break
				}
				rows = append(rows, blocks[cursor])
				cursor++
			}
			created, err := splitTable(markdown, block, rows, tableID, config.ChunkSize)
			if err != nil {
				return nil, err
			}
			drafts = append(drafts, created...)
			index = cursor
			continue
		}
		if len(ordinary) > 0 && isStrategyBoundary(strategy, ordinary[0], block) {
			if err := flushOrdinary(); err != nil {
				return nil, err
			}
		}
		ordinary = append(ordinary, block)
		index++
	}
	if err := flushOrdinary(); err != nil {
		return nil, err
	}
	return drafts, nil
}

func isStrategyBoundary(strategy value.ChunkingStrategy, first, current model.ParsedBlock) bool {
	switch strategy {
	case value.ChunkingStrategyHeading:
		return current.Kind == model.BlockKindHeading || !sameStrings(first.HeadingPath, current.HeadingPath)
	case value.ChunkingStrategyHeuristic:
		return current.Kind == model.BlockKindHeading || current.Kind == model.BlockKindThematicBreak
	default:
		return false
	}
}

func materializeChunkDrafts(input ChunkInput, drafts []chunkDraft, role value.ChunkRole, parentID *uuid.UUID) ([]*model.Chunk, []*model.ChunkRevision, error) {
	now := time.Now().UTC()
	chunks := make([]*model.Chunk, len(drafts))
	revisions := make([]*model.ChunkRevision, len(drafts))
	for sequence, draft := range drafts {
		metadata := copyChunkMetadata(draft.metadata)
		metadata["parser"] = input.Manifest.Parser
		metadata["manifest_version"] = input.Manifest.Version
		header := contextHeader(input.Title, draft.headingPath, draft.anchor.Sheet)
		embedding := draft.content
		if header != "" {
			embedding = header + "\n\n" + draft.content
		}
		chunkID := id.New()
		revision, err := model.NewChunkRevision(model.NewChunkRevisionInput{
			WorkspaceID: input.WorkspaceID, KnowledgeBaseID: input.KnowledgeBaseID,
			DocumentID: input.DocumentID, DocumentRevisionID: input.DocumentRevisionID,
			ChunkSetID: input.ChunkSetID, ChunkID: chunkID, RevisionNo: 1,
			Content: draft.content, ContextHeader: header, EmbeddingContent: embedding,
			Enabled: true, Status: value.ChunkRevisionPending, EditSource: value.ChunkEditSourceSystem,
		})
		if err != nil {
			return nil, nil, err
		}
		revisionID := revision.ID
		chunks[sequence] = &model.Chunk{
			ID: chunkID, WorkspaceID: input.WorkspaceID, KnowledgeBaseID: input.KnowledgeBaseID,
			DocumentID: input.DocumentID, DocumentRevisionID: input.DocumentRevisionID,
			ChunkSetID: input.ChunkSetID, Role: role, ParentChunkID: parentID, Sequence: sequence, SourceContent: draft.content,
			ActiveRevisionID: &revisionID,
			Content:          draft.content, ContextHeader: header, EmbeddingContent: embedding,
			SourceAnchor: draft.anchor, Metadata: metadata, CreatedAt: now,
		}
		revisions[sequence] = revision
	}
	return chunks, revisions, nil
}

func materializeParentChildDrafts(input ChunkInput, drafts []chunkDraft, config value.ChunkingConfig) ([]*model.Chunk, []*model.ChunkRevision, error) {
	if len(drafts) == 0 {
		return []*model.Chunk{}, []*model.ChunkRevision{}, nil
	}
	chunks := make([]*model.Chunk, 0, len(drafts)*2)
	revisions := make([]*model.ChunkRevision, 0, len(drafts)*2)
	for start := 0; start < len(drafts); {
		end := start + 1
		size := utf8.RuneCountInString(drafts[start].content)
		for end < len(drafts) && sameStrings(drafts[start].headingPath, drafts[end].headingPath) {
			next := utf8.RuneCountInString(drafts[end].content)
			if size+2+next > config.ParentChunkSize && end > start {
				break
			}
			size += 2 + next
			end++
		}
		parentDraft := drafts[start]
		parts := make([]string, 0, end-start)
		for _, draft := range drafts[start:end] {
			parts = append(parts, draft.content)
		}
		parentDraft.content = strings.Join(parts, "\n\n")
		parentID := id.New()
		parentChunks, parentRevisions, err := materializeChunkDrafts(input, []chunkDraft{parentDraft}, value.ChunkRoleParent, nil)
		if err != nil {
			return nil, nil, err
		}
		parentChunks[0].ID = parentID
		parentRevisions[0].ChunkID = parentID
		parentChunks[0].ActiveRevisionID = &parentRevisions[0].ID
		parentRevisions[0].Status = value.ChunkRevisionReady
		children, childRevisions, err := materializeChunkDrafts(input, drafts[start:end], value.ChunkRoleChild, &parentID)
		if err != nil {
			return nil, nil, err
		}
		for index := range children {
			children[index].Sequence = start + index
		}
		parentChunks[0].Sequence = start
		chunks = append(chunks, parentChunks[0])
		chunks = append(chunks, children...)
		revisions = append(revisions, parentRevisions[0])
		revisions = append(revisions, childRevisions...)
		start = end
	}
	return chunks, revisions, nil
}

func splitOrdinary(markdown string, blocks []model.ParsedBlock, config value.ChunkingConfig) ([]chunkDraft, error) {
	var combined strings.Builder
	ranges := make([]blockRange, 0, len(blocks))
	for index, block := range blocks {
		if index > 0 {
			combined.WriteString("\n\n")
		}
		start := utf8.RuneCountInString(combined.String())
		combined.WriteString(markdown[block.NormalizedStart:block.NormalizedEnd])
		ranges = append(ranges, blockRange{block: block, runeStart: start, runeEnd: utf8.RuneCountInString(combined.String())})
	}
	runes := []rune(combined.String())
	if len(runes) == 0 {
		return nil, nil
	}
	headingBoundary := 0
	if blocks[0].Kind == model.BlockKindHeading {
		headingBoundary = ranges[0].runeEnd
		if len(blocks) > 1 {
			headingBoundary += 2
		}
	}
	drafts := make([]chunkDraft, 0)
	for start := 0; start < len(runes); {
		end := start + config.ChunkSize
		if end > len(runes) {
			end = len(runes)
		} else {
			end = preferredBoundary(runes, start, end)
		}
		if end <= start {
			end = minInt(start+config.ChunkSize, len(runes))
		}
		content := strings.TrimSpace(string(runes[start:end]))
		if content != "" {
			anchor, metadata, err := windowDetails(ranges, start, end)
			if err != nil {
				return nil, err
			}
			metadata["parser"] = "structured"
			metadata["manifest_version"] = model.CurrentParseManifestVersion
			drafts = append(drafts, chunkDraft{content: content, headingPath: blocks[0].HeadingPath, anchor: anchor, metadata: metadata})
		}
		if end == len(runes) {
			break
		}
		next := end - config.ChunkOverlap
		if next < 0 {
			next = 0
		}
		if headingBoundary > 0 && headingBoundary <= end && next < headingBoundary {
			next = headingBoundary
		}
		if next <= start {
			next = end
		}
		start = next
	}
	return drafts, nil
}

func splitTable(markdown string, header model.ParsedBlock, rows []model.ParsedBlock, tableID string, size int) ([]chunkDraft, error) {
	headerContent := markdown[header.NormalizedStart:header.NormalizedEnd]
	metadataBase := map[string]any{
		"parser": "structured", "manifest_version": model.CurrentParseManifestVersion,
		"block_kind": "table", "table_id": tableID,
	}
	if len(rows) == 0 {
		return []chunkDraft{{content: headerContent, headingPath: header.HeadingPath, anchor: header.SourceAnchor, metadata: metadataBase}}, nil
	}
	drafts := make([]chunkDraft, 0)
	current := make([]model.ParsedBlock, 0)
	flush := func(group []model.ParsedBlock) error {
		if len(group) == 0 {
			return nil
		}
		parts := []string{headerContent}
		for _, row := range group {
			parts = append(parts, markdown[row.NormalizedStart:row.NormalizedEnd])
		}
		content := strings.Join(parts, "\n\n")
		anchor, err := mergeBlockAnchors(group)
		if err != nil {
			return err
		}
		if anchor.HeaderRow == nil {
			anchor.HeaderRow = cloneInt(header.SourceAnchor.HeaderRow)
		}
		metadata := copyChunkMetadata(metadataBase)
		if utf8.RuneCountInString(content) > size {
			metadata["oversized"] = true
		}
		drafts = append(drafts, chunkDraft{content: content, headingPath: header.HeadingPath, anchor: anchor, metadata: metadata})
		return nil
	}
	for _, row := range rows {
		candidateParts := []string{headerContent}
		for _, existing := range current {
			candidateParts = append(candidateParts, markdown[existing.NormalizedStart:existing.NormalizedEnd])
		}
		candidateParts = append(candidateParts, markdown[row.NormalizedStart:row.NormalizedEnd])
		if len(current) > 0 && utf8.RuneCountInString(strings.Join(candidateParts, "\n\n")) > size {
			if err := flush(current); err != nil {
				return nil, err
			}
			current = current[:0]
		}
		current = append(current, row)
	}
	if err := flush(current); err != nil {
		return nil, err
	}
	return drafts, nil
}

func preferredBoundary(runes []rune, start, limit int) int {
	minimum := start + (limit-start)/2
	for _, predicate := range []func([]rune, int) bool{
		func(value []rune, index int) bool {
			return index >= 2 && value[index-2] == '\n' && value[index-1] == '\n'
		},
		func(value []rune, index int) bool { return index >= 1 && value[index-1] == '\n' },
		func(value []rune, index int) bool {
			return index >= 1 && strings.ContainsRune("。！？.!?；;", value[index-1])
		},
	} {
		for index := limit; index > minimum; index-- {
			if predicate(runes, index) {
				return index
			}
		}
	}
	return limit
}

func windowDetails(ranges []blockRange, start, end int) (value.SourceAnchor, map[string]any, error) {
	selected := make([]model.ParsedBlock, 0)
	splitBlock := false
	for _, item := range ranges {
		if item.runeEnd <= start || item.runeStart >= end {
			continue
		}
		selected = append(selected, item.block)
		if start > item.runeStart || end < item.runeEnd {
			splitBlock = true
		}
	}
	anchor, err := mergeBlockAnchors(selected)
	if err != nil {
		return value.SourceAnchor{}, nil, err
	}
	metadata := map[string]any{}
	if len(selected) == 1 {
		metadata["block_kind"] = string(selected[0].Kind)
	} else {
		metadata["block_kind"] = "mixed"
	}
	if splitBlock {
		metadata["anchor_granularity"] = "block"
		if len(selected) == 1 && selected[0].Kind == model.BlockKindCode {
			metadata["code_block_sequence"] = selected[0].Sequence
		}
	}
	return anchor, metadata, nil
}

func mergeBlockAnchors(blocks []model.ParsedBlock) (value.SourceAnchor, error) {
	if len(blocks) == 0 {
		return value.SourceAnchor{}, nil
	}
	anchor := blocks[0].SourceAnchor
	for _, block := range blocks[1:] {
		merged, err := value.MergeSourceAnchors(anchor, block.SourceAnchor)
		if err != nil {
			return value.SourceAnchor{}, err
		}
		anchor = merged
	}
	return anchor, nil
}

func contextHeader(title string, path []string, sheet string) string {
	parts := make([]string, 0, 1+len(path))
	appendUnique := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		for _, existing := range parts {
			if strings.EqualFold(existing, value) {
				return
			}
		}
		parts = append(parts, value)
	}
	appendUnique(title)
	for _, heading := range path {
		appendUnique(heading)
	}
	appendUnique(sheet)
	return strings.Join(parts, " > ")
}

func sameStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func copyChunkMetadata(source map[string]any) map[string]any {
	copy := make(map[string]any, len(source))
	for key, value := range source {
		copy[key] = value
	}
	return copy
}

func cloneInt(value *int) *int {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}
