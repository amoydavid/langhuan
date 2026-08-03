package db

import (
	"errors"
	"fmt"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
)

const undefinedObjectSQLState = "42704"

type sqlStateError interface {
	SQLState() string
}

func translateFTSConfigError(err error, ftsConfig, wrap string) error {
	if err == nil {
		return nil
	}
	var stateError sqlStateError
	if errors.As(err, &stateError) && stateError.SQLState() == undefinedObjectSQLState {
		return fmt.Errorf(
			"%w: 全文检索配置 %q 不存在",
			domainerrors.ErrValidation,
			ftsConfig,
		)
	}
	return translateDBError(err, wrap)
}
