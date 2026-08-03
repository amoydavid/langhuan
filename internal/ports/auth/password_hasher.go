// Package auth 定义认证相关的端口接口。
package auth

// PasswordHasher 描述密码哈希与校验的端口抽象。
// 实现必须使用 argon2id 派生算法，且不得记录或返回明文密码。
type PasswordHasher interface {
	// Hash 对明文密码进行 argon2id 哈希，返回标准编码字符串。
	Hash(password string) (string, error)
	// Verify 校验编码哈希与明文密码是否匹配。
	// 匹配返回 true；不匹配或格式错误返回 false（调用方按"密码错误"处理）。
	Verify(encodedHash, password string) (bool, error)
	// VerifyDummy 使用预先生成的 dummy 哈希执行一次完整 argon2id 校验，
	// 用以在不存在的用户登录时消耗与真实校验相同的时间，防止用户枚举。
	// 始终返回 nil。
	VerifyDummy(password string) error
}
