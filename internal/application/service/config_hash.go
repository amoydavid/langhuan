package service

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
)

// CanonicalConfigHash returns a deterministic SHA-256 fingerprint of JSON data.
func CanonicalConfigHash(value map[string]any) (string, error) {
	encoded, err := canonicalJSON(value)
	if err != nil {
		return "", fmt.Errorf("编码配置指纹失败: %w", err)
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

func canonicalJSON(value any) ([]byte, error) {
	var buffer bytes.Buffer
	if err := writeCanonicalJSON(&buffer, value); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func writeCanonicalJSON(buffer *bytes.Buffer, value any) error {
	switch typed := value.(type) {
	case nil:
		buffer.WriteString("null")
	case bool:
		buffer.WriteString(strconv.FormatBool(typed))
	case string:
		encoded, _ := json.Marshal(typed)
		buffer.Write(encoded)
	case json.Number:
		number, err := typed.Float64()
		if err != nil {
			return fmt.Errorf("无效 JSON number %q: %w", typed, err)
		}
		if math.IsNaN(number) || math.IsInf(number, 0) {
			return fmt.Errorf("无效 JSON number %q", typed)
		}
		buffer.WriteString(typed.String())
	case float64:
		if math.IsNaN(typed) || math.IsInf(typed, 0) {
			return fmt.Errorf("JSON 不支持 NaN 或 Inf")
		}
		buffer.WriteString(strconv.FormatFloat(typed, 'g', -1, 64))
	case float32:
		return writeCanonicalJSON(buffer, float64(typed))
	case int:
		buffer.WriteString(strconv.Itoa(typed))
	case int8:
		buffer.WriteString(strconv.FormatInt(int64(typed), 10))
	case int16:
		buffer.WriteString(strconv.FormatInt(int64(typed), 10))
	case int32:
		buffer.WriteString(strconv.FormatInt(int64(typed), 10))
	case int64:
		buffer.WriteString(strconv.FormatInt(typed, 10))
	case uint:
		buffer.WriteString(strconv.FormatUint(uint64(typed), 10))
	case uint8:
		buffer.WriteString(strconv.FormatUint(uint64(typed), 10))
	case uint16:
		buffer.WriteString(strconv.FormatUint(uint64(typed), 10))
	case uint32:
		buffer.WriteString(strconv.FormatUint(uint64(typed), 10))
	case uint64:
		buffer.WriteString(strconv.FormatUint(typed, 10))
	case []any:
		buffer.WriteByte('[')
		for index, item := range typed {
			if index > 0 {
				buffer.WriteByte(',')
			}
			if err := writeCanonicalJSON(buffer, item); err != nil {
				return err
			}
		}
		buffer.WriteByte(']')
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		buffer.WriteByte('{')
		for index, key := range keys {
			if index > 0 {
				buffer.WriteByte(',')
			}
			encodedKey, _ := json.Marshal(key)
			buffer.Write(encodedKey)
			buffer.WriteByte(':')
			if err := writeCanonicalJSON(buffer, typed[key]); err != nil {
				return err
			}
		}
		buffer.WriteByte('}')
	default:
		return fmt.Errorf("不支持的 JSON 值类型 %T", value)
	}
	return nil
}
