// Package auth 提供 internal/ports/auth 端口的具体适配器实现。
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"

	authport "github.com/dajee/langhuan/internal/ports/auth"
)

// 编译期断言：Argon2Hasher 实现端口 auth.PasswordHasher。
var _ authport.PasswordHasher = (*Argon2Hasher)(nil)

// Argon2Hasher 使用 argon2id 实现端口 auth.PasswordHasher。
// 参数作为具名字段，避免在热路径中出现魔法数字。
type Argon2Hasher struct {
	memory      uint32 // 内存（KiB）
	iterations  uint32 // 迭代次数
	parallelism uint8  // 并行度
	saltLength  uint32 // 盐长度（字节）
	keyLength   uint32 // 派生 key 长度（字节）
	// dummyHash 在构造器中预生成一次，VerifyDummy 复用其完整派生过程
	// 以模拟真实校验耗时，防止用户枚举的时序攻击。
	dummyHash string
}

// NewArgon2Hasher 构造一个 Argon2Hasher，盐长度默认 16 字节，派生 key 长度默认 32 字节。
// memory 单位为 KiB，与 config.PasswordConfig.Argon2MemoryKiB 对齐。
func NewArgon2Hasher(memory, iterations uint32, parallelism uint8) *Argon2Hasher {
	h := &Argon2Hasher{
		memory:      memory,
		iterations:  iterations,
		parallelism: parallelism,
		saltLength:  16,
		keyLength:   32,
	}
	// 预生成 dummy 哈希一次：VerifyDummy 复用同样参数的完整派生流程。
	h.dummyHash = h.mustGenerateDummy()
	return h
}

// Hash 使用 crypto/rand 生成随机盐，派生 argon2id key 并编码为标准字符串：
// $argon2id$v=19$m=<memory>,t=<iterations>,p=<parallelism>$<base64(salt)>$<base64(key)>
func (h *Argon2Hasher) Hash(password string) (string, error) {
	salt, err := randomSalt(h.saltLength)
	if err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, h.iterations, h.memory, h.parallelism, h.keyLength)
	return h.encode(salt, key), nil
}

// Verify 解析编码哈希，用解析出的参数重新派生 key 并做常量时间比较。
// 匹配返回 (true, nil)；不匹配返回 (false, nil)；编码格式错误返回 (false, nil)，
// 以便调用方统一按"密码错误"处理。
func (h *Argon2Hasher) Verify(encodedHash, password string) (bool, error) {
	salt, key, params, err := decodeHash(encodedHash)
	if err != nil {
		return false, nil
	}

	otherKey := argon2.IDKey([]byte(password), salt, params.iterations, params.memory, params.parallelism, uint32(len(key)))
	return subtle.ConstantTimeCompare(key, otherKey) == 1, nil
}

// VerifyDummy 对预生成的 dummy 哈希执行一次完整 argon2id 派生与比较，
// 用以在登录不存在的用户时消耗与真实校验相近的时间。结果被丢弃，始终返回 nil。
func (h *Argon2Hasher) VerifyDummy(password string) error {
	_, _ = h.Verify(h.dummyHash, password)
	return nil
}

// encode 将盐与派生 key 拼装为 argon2id 标准编码字符串。
// 使用 base64.RawStdEncoding（无 padding）以匹配 de-facto 标准的可往返格式。
func (h *Argon2Hasher) encode(salt, key []byte) string {
	b64salt := base64.RawStdEncoding.EncodeToString(salt)
	b64key := base64.RawStdEncoding.EncodeToString(key)
	return fmt.Sprintf(
		"$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s",
		h.memory, h.iterations, h.parallelism, b64salt, b64key,
	)
}

// argon2Params 保存从编码字符串解析出的参数。
type argon2Params struct {
	memory      uint32
	iterations  uint32
	parallelism uint8
}

// decodeHash 解析 $argon2id$v=19$m=<>,t=<>,p=<>$<salt>$<key> 字符串。
func decodeHash(encodedHash string) (salt, key []byte, params argon2Params, err error) {
	parts := strings.Split(encodedHash, "$")
	// Split 后：["", "argon2id", "v=19", "m=..,t=..,p=..", "<salt>", "<key>"]
	if len(parts) != 6 {
		return nil, nil, params, errors.New("argon2id: 编码字符串字段数不正确")
	}
	if parts[1] != "argon2id" {
		return nil, nil, params, fmt.Errorf("argon2id: 期望算法 argon2id, 实际 %q", parts[1])
	}
	if parts[2] != "v=19" {
		return nil, nil, params, fmt.Errorf("argon2id: 期望版本 v=19, 实际 %q", parts[2])
	}

	params, err = parseArgon2Params(parts[3])
	if err != nil {
		return nil, nil, params, err
	}

	salt, err = base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return nil, nil, params, fmt.Errorf("argon2id: 解码盐失败: %w", err)
	}
	key, err = base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return nil, nil, params, fmt.Errorf("argon2id: 解码 key 失败: %w", err)
	}
	return salt, key, params, nil
}

// parseArgon2Params 解析 "m=65536,t=3,p=2" 形式的参数段。
func parseArgon2Params(segment string) (argon2Params, error) {
	var params argon2Params
	for _, kv := range strings.Split(segment, ",") {
		parts := strings.SplitN(kv, "=", 2)
		if len(parts) != 2 {
			return params, fmt.Errorf("argon2id: 参数段格式错误 %q", kv)
		}
		val, err := strconv.ParseUint(parts[1], 10, 32)
		if err != nil {
			return params, fmt.Errorf("argon2id: 参数值 %q 解析失败: %w", parts[1], err)
		}
		switch parts[0] {
		case "m":
			params.memory = uint32(val)
		case "t":
			params.iterations = uint32(val)
		case "p":
			if val == 0 || val > 255 {
				return params, fmt.Errorf("argon2id: parallelism 超出 uint8 范围: %d", val)
			}
			params.parallelism = uint8(val)
		default:
			return params, fmt.Errorf("argon2id: 未知参数 %q", parts[0])
		}
	}
	if params.memory == 0 || params.iterations == 0 || params.parallelism == 0 {
		return params, errors.New("argon2id: 参数段缺少 m/t/p 之一")
	}
	return params, nil
}

// randomSalt 从 crypto/rand 读取指定长度的盐。
func randomSalt(length uint32) ([]byte, error) {
	salt := make([]byte, length)
	if _, err := rand.Read(salt); err != nil {
		return nil, fmt.Errorf("argon2id: 生成随机盐失败: %w", err)
	}
	return salt, nil
}

// mustGenerateDummy 在构造器中预生成 dummy 哈希。该哈希仅用于消耗
// 与真实校验相同的 argon2id 计算时间，其值无安全意义。
// 哈希失败（crypto/rand 故障）属于不可恢复错误，直接 panic。
func (h *Argon2Hasher) mustGenerateDummy() string {
	encoded, err := h.Hash("langhuan-dummy-hash-for-timing-parity")
	if err != nil {
		panic(fmt.Sprintf("argon2id: 预生成 dummy 哈希失败: %v", err))
	}
	return encoded
}
