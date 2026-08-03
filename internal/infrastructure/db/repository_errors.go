package db

import (
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
)

var ErrRepositoryNotFound = domainerrors.ErrNotFound

// translateDBError 将 GORM/driver 错误映射为领域错误，绝不泄漏底层 error。
// 依赖 Open() 开启的 TranslateError：GORM 会把唯一约束冲突翻译成
// gorm.ErrDuplicatedKey。这里再做一道 *pgconn.PgError (SQLSTATE 23505)
// 的类型检查作为双保险，最后保留字符串兜底以应对未翻译的错误。
func translateDBError(err error, wrap string) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return domainerrors.ErrNotFound
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return domainerrors.ErrConflict
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		// 23505 = unique_violation（唯一约束/重复键）。
		return domainerrors.ErrConflict
	}
	// 字符串兜底：应对未被翻译的 driver 错误。
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "23505") || strings.Contains(msg, "unique constraint") ||
		strings.Contains(msg, "duplicate key") {
		return domainerrors.ErrConflict
	}
	return fmt.Errorf("%s: %w", wrap, err)
}

func isForeignKeyViolation(err error) bool {
	if errors.Is(err, gorm.ErrForeignKeyViolated) {
		return true
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23503" {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "23503") || strings.Contains(msg, "foreign key constraint")
}
