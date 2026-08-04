package db

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

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
	// 复用 model 层的结构校验，保证 parser/block/source anchor 白名单单一来源，
	// 避免 codec 与 ParseManifest.Validate 各自维护导致漂移。
	return manifest.ValidateStructure()
}

func isZeroParseManifest(manifest model.ParseManifest) bool {
	return manifest.Version == 0 && manifest.Parser == "" && manifest.ParserVersion == 0 && len(manifest.Blocks) == 0 && len(manifest.Warnings) == 0
}
