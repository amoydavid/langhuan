// Package id 提供统一的服务端持久化资源 ID 生成器。
// 所有需要落库、入队或跨服务引用的业务资源 ID 都应使用 id.New()，
// 保证全局有序、可排序且不会在分布式环境中碰撞。
package id

import (
	"fmt"

	"github.com/google/uuid"
)

// New 返回一个新的 UUIDv7。
// UUIDv7 以 Unix 毫秒时间戳为前缀，天然按创建时间排序，适合用作数据库主键。
func New() uuid.UUID {
	generated, err := uuid.NewV7()
	if err != nil {
		// 仅当系统随机源不可用时才可能失败，属于不可恢复的程序错误。
		panic(fmt.Sprintf("生成 UUIDv7 失败: %v", err))
	}
	return generated
}
