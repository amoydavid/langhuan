package auth

import (
	"strings"
	"testing"
)

// newLowCostHasher 使用极小参数构造 argon2 哈希器，保证测试在毫秒级完成。
// 运行时默认参数（memory=65536/iterations=3/parallelism=2）由配置注入，
// 测试不使用默认参数以避免每次哈希 ~50-100ms 的开销。
func newLowCostHasher() *Argon2Hasher {
	return NewArgon2Hasher(128, 1, 1)
}

func TestArgon2HasherHashAndVerify(t *testing.T) {
	hasher := newLowCostHasher()
	const password = "correct horse battery staple"

	encoded, err := hasher.Hash(password)
	if err != nil {
		t.Fatalf("Hash 返回错误: %v", err)
	}

	// 1. 编码字符串具备 argon2id 标准前缀。
	if !strings.HasPrefix(encoded, "$argon2id$v=19$m=") {
		t.Fatalf("哈希前缀异常: %q", encoded)
	}

	// 2. 正确密码校验通过。
	ok, err := hasher.Verify(encoded, password)
	if err != nil {
		t.Fatalf("Verify 返回错误: %v", err)
	}
	if !ok {
		t.Fatal("Verify 对正确密码应返回 true")
	}

	// 3. 错误密码校验失败。
	ok, err = hasher.Verify(encoded, "wrong-password")
	if err != nil {
		t.Fatalf("Verify 返回错误: %v", err)
	}
	if ok {
		t.Fatal("Verify 对错误密码应返回 false")
	}

	// 4. 两次 Hash 因随机盐产生不同结果。
	other, err := hasher.Hash(password)
	if err != nil {
		t.Fatalf("第二次 Hash 返回错误: %v", err)
	}
	if other == encoded {
		t.Fatal("两次 Hash 因随机盐应产生不同编码字符串")
	}

	// 5. 畸形编码应按"不匹配"处理，返回 false。
	ok, err = hasher.Verify("not-a-valid-hash", password)
	if ok || err != nil {
		t.Fatalf("Verify 对畸形编码应返回 (false, nil), got (%v, %v)", ok, err)
	}
}

func TestArgon2HasherVerifyDummy(t *testing.T) {
	hasher := newLowCostHasher()

	// VerifyDummy 必须实际执行一次 argon2 派生（非 no-op）且不报错。
	if err := hasher.VerifyDummy("any-password"); err != nil {
		t.Fatalf("VerifyDummy 返回错误: %v", err)
	}
	if err := hasher.VerifyDummy("different-password"); err != nil {
		t.Fatalf("VerifyDummy 返回错误: %v", err)
	}

	// dummy 哈希应在构造器中预生成一次，而非每次调用时重新生成。
	if hasher.dummyHash == "" {
		t.Fatal("dummyHash 未在构造器中预生成")
	}
}
