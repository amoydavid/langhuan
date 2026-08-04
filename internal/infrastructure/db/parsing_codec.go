package db

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

type sourceAnchorJSON struct {
	SourceType     string `json:"source_type"`
	OffsetStart    *int   `json:"offset_start,omitempty"`
	OffsetEnd      *int   `json:"offset_end,omitempty"`
	LineStart      *int   `json:"line_start,omitempty"`
	LineEnd        *int   `json:"line_end,omitempty"`
	Sheet          string `json:"sheet,omitempty"`
	HeaderRow      *int   `json:"header_row,omitempty"`
	RowStart       *int   `json:"row_start,omitempty"`
	RowEnd         *int   `json:"row_end,omitempty"`
	ColumnStart    *int   `json:"column_start,omitempty"`
	ColumnEnd      *int   `json:"column_end,omitempty"`
	ParagraphStart *int   `json:"paragraph_start,omitempty"`
	ParagraphEnd   *int   `json:"paragraph_end,omitempty"`
	TableIndex     *int   `json:"table_index,omitempty"`
}

type parsedBlockJSON struct {
	Sequence        int              `json:"sequence"`
	Kind            model.BlockKind  `json:"kind"`
	NormalizedStart int              `json:"normalized_start"`
	NormalizedEnd   int              `json:"normalized_end"`
	HeadingPath     []string         `json:"heading_path"`
	SourceAnchor    sourceAnchorJSON `json:"source_anchor"`
	Metadata        map[string]any   `json:"metadata"`
}

type parseWarningJSON struct {
	Code         string           `json:"code"`
	Message      string           `json:"message"`
	SourceAnchor sourceAnchorJSON `json:"source_anchor"`
}

type parseManifestJSON struct {
	Version       int                `json:"version"`
	Parser        string             `json:"parser"`
	ParserVersion int                `json:"parser_version"`
	Blocks        []parsedBlockJSON  `json:"blocks"`
	Warnings      []parseWarningJSON `json:"warnings"`
}

func parseManifestToJSONMap(manifest model.ParseManifest) (JSONMap, error) {
	if isZeroParseManifest(manifest) {
		return JSONMap{}, nil
	}
	if err := validateManifestForCodec(manifest); err != nil {
		return nil, fmt.Errorf("编码 parse manifest 失败: %w", err)
	}

	encoded := parseManifestJSON{
		Version:       manifest.Version,
		Parser:        manifest.Parser,
		ParserVersion: manifest.ParserVersion,
		Blocks:        make([]parsedBlockJSON, len(manifest.Blocks)),
	}
	if manifest.Warnings != nil {
		encoded.Warnings = make([]parseWarningJSON, len(manifest.Warnings))
	}
	for i, block := range manifest.Blocks {
		encoded.Blocks[i] = parsedBlockJSON{
			Sequence:        block.Sequence,
			Kind:            block.Kind,
			NormalizedStart: block.NormalizedStart,
			NormalizedEnd:   block.NormalizedEnd,
			HeadingPath:     block.HeadingPath,
			SourceAnchor:    sourceAnchorToJSON(block.SourceAnchor),
			Metadata:        block.Metadata,
		}
	}
	for i, warning := range manifest.Warnings {
		encoded.Warnings[i] = parseWarningJSON{
			Code:         warning.Code,
			Message:      warning.Message,
			SourceAnchor: sourceAnchorToJSON(warning.SourceAnchor),
		}
	}
	return structToJSONMap(encoded)
}

func parseManifestFromJSONMap(raw JSONMap) (model.ParseManifest, error) {
	if len(raw) == 0 {
		return model.ParseManifest{}, nil
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return model.ParseManifest{}, fmt.Errorf("编码 parse manifest JSONB 失败: %w", err)
	}
	var decoded parseManifestJSON
	if err := decodeStrictJSON(data, &decoded); err != nil {
		return model.ParseManifest{}, fmt.Errorf("解码 parse manifest JSONB 失败: %w", err)
	}

	manifest := model.ParseManifest{
		Version:       decoded.Version,
		Parser:        decoded.Parser,
		ParserVersion: decoded.ParserVersion,
		Blocks:        make([]model.ParsedBlock, len(decoded.Blocks)),
	}
	if decoded.Warnings != nil {
		manifest.Warnings = make([]model.ParseWarning, len(decoded.Warnings))
	}
	for i, block := range decoded.Blocks {
		anchor := sourceAnchorFromJSON(block.SourceAnchor)
		manifest.Blocks[i] = model.ParsedBlock{
			Sequence:        block.Sequence,
			Kind:            block.Kind,
			NormalizedStart: block.NormalizedStart,
			NormalizedEnd:   block.NormalizedEnd,
			HeadingPath:     block.HeadingPath,
			SourceAnchor:    anchor,
			Metadata:        block.Metadata,
		}
	}
	for i, warning := range decoded.Warnings {
		manifest.Warnings[i] = model.ParseWarning{
			Code:         warning.Code,
			Message:      warning.Message,
			SourceAnchor: sourceAnchorFromJSON(warning.SourceAnchor),
		}
	}
	if err := validateManifestForCodec(manifest); err != nil {
		return model.ParseManifest{}, fmt.Errorf("解码 parse manifest JSONB 失败: %w", err)
	}
	return manifest, nil
}

func sourceAnchorToJSONMap(anchor value.SourceAnchor) JSONMap {
	raw, err := structToJSONMap(sourceAnchorToJSON(anchor))
	if err != nil {
		return JSONMap{}
	}
	return raw
}

func sourceAnchorFromJSONMap(raw JSONMap) (value.SourceAnchor, error) {
	if len(raw) == 0 {
		return value.SourceAnchor{}, nil
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return value.SourceAnchor{}, fmt.Errorf("编码 source anchor JSONB 失败: %w", err)
	}
	var decoded sourceAnchorJSON
	if err := decodeStrictJSON(data, &decoded); err != nil {
		return value.SourceAnchor{}, fmt.Errorf("解码 source anchor JSONB 失败: %w", err)
	}
	anchor := sourceAnchorFromJSON(decoded)
	if err := anchor.Validate(); err != nil {
		return value.SourceAnchor{}, fmt.Errorf("解码 source anchor JSONB 失败: %w", err)
	}
	return anchor, nil
}

func sourceAnchorToJSON(anchor value.SourceAnchor) sourceAnchorJSON {
	return sourceAnchorJSON{
		SourceType: anchor.SourceType, OffsetStart: anchor.OffsetStart, OffsetEnd: anchor.OffsetEnd,
		LineStart: anchor.LineStart, LineEnd: anchor.LineEnd, Sheet: anchor.Sheet,
		HeaderRow: anchor.HeaderRow, RowStart: anchor.RowStart, RowEnd: anchor.RowEnd,
		ColumnStart: anchor.ColumnStart, ColumnEnd: anchor.ColumnEnd,
		ParagraphStart: anchor.ParagraphStart, ParagraphEnd: anchor.ParagraphEnd, TableIndex: anchor.TableIndex,
	}
}

func sourceAnchorFromJSON(anchor sourceAnchorJSON) value.SourceAnchor {
	return value.SourceAnchor{
		SourceType: anchor.SourceType, OffsetStart: anchor.OffsetStart, OffsetEnd: anchor.OffsetEnd,
		LineStart: anchor.LineStart, LineEnd: anchor.LineEnd, Sheet: anchor.Sheet,
		HeaderRow: anchor.HeaderRow, RowStart: anchor.RowStart, RowEnd: anchor.RowEnd,
		ColumnStart: anchor.ColumnStart, ColumnEnd: anchor.ColumnEnd,
		ParagraphStart: anchor.ParagraphStart, ParagraphEnd: anchor.ParagraphEnd, TableIndex: anchor.TableIndex,
	}
}

func structToJSONMap(input any) (JSONMap, error) {
	data, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	var raw JSONMap
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	return raw, nil
}

func decodeStrictJSON(data []byte, output any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(output); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return fmt.Errorf("JSON 包含多个值")
		}
		return err
	}
	return nil
}

func validateManifestForCodec(manifest model.ParseManifest) error {
	if manifest.Version != model.CurrentParseManifestVersion {
		return fmt.Errorf("%w: 不支持的 parse manifest version: %d", domainerrors.ErrValidation, manifest.Version)
	}
	if !model.IsKnownParser(manifest.Parser) {
		return fmt.Errorf("%w: 未知 parser: %q", domainerrors.ErrValidation, manifest.Parser)
	}
	if manifest.ParserVersion <= 0 {
		return fmt.Errorf("%w: parser_version 必须大于 0", domainerrors.ErrValidation)
	}
	if len(manifest.Blocks) == 0 {
		return fmt.Errorf("%w: parse manifest 至少需要一个 block", domainerrors.ErrValidation)
	}
	previousEnd := 0
	for i, block := range manifest.Blocks {
		if block.Sequence != i {
			return fmt.Errorf("%w: block sequence 必须从 0 连续递增", domainerrors.ErrValidation)
		}
		switch block.Kind {
		case model.BlockKindHeading, model.BlockKindParagraph, model.BlockKindList, model.BlockKindQuote,
			model.BlockKindCode, model.BlockKindThematicBreak, model.BlockKindTableHeader, model.BlockKindTableRow:
		default:
			return fmt.Errorf("%w: 未知 block kind: %q", domainerrors.ErrValidation, block.Kind)
		}
		if block.NormalizedStart < previousEnd || block.NormalizedEnd <= block.NormalizedStart {
			return fmt.Errorf("%w: block %d span 非法", domainerrors.ErrValidation, i)
		}
		if err := block.SourceAnchor.Validate(); err != nil {
			return fmt.Errorf("block %d source anchor: %w", i, err)
		}
		previousEnd = block.NormalizedEnd
	}
	for i, warning := range manifest.Warnings {
		if err := warning.SourceAnchor.Validate(); err != nil {
			return fmt.Errorf("warning %d source anchor: %w", i, err)
		}
	}
	return nil
}

func isZeroParseManifest(manifest model.ParseManifest) bool {
	return manifest.Version == 0 && manifest.Parser == "" && manifest.ParserVersion == 0 && len(manifest.Blocks) == 0 && len(manifest.Warnings) == 0
}
