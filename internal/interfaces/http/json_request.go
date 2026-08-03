package http

import (
	"encoding/json"
	"errors"
	"io"

	"github.com/gin-gonic/gin"
)

func decodeStrictJSON(c *gin.Context, destination any) error {
	decoder := json.NewDecoder(c.Request.Body)
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
