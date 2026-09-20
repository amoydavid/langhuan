package http

import (
	"encoding/json"
	"errors"
	"io"

	"github.com/gin-gonic/gin"
)

func decodeStrictJSON(c *gin.Context, destination any) error {
	return decodeStrictJSONReader(c.Request.Body, destination)
}

// decodeStrictJSONReader 严格解析单个 JSON 对象：拒绝未知字段与多个 JSON 对象。
// 供 request body 可选的端点在 io.ReadAll 后复用（如 web 源手动同步）。
func decodeStrictJSONReader(body io.Reader, destination any) error {
	decoder := json.NewDecoder(body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}

	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("请求只能包含一个 JSON 对象")
		}
		return err
	}
	return nil
}
