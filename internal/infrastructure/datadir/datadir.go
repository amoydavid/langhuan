// Package datadir 准备 standalone 模式的持久数据目录与凭证密钥文件。
//
// 它负责：
//   - 解析用户主目录下的 ~/.langhuan-data 数据根目录（0700）
//   - 首次生成或复用独立的 credential.key（0600，32 字节 Base64）
//
// 密钥与配置分离：credential.key 是独立文件，绝不由本包写入任何 config 文本。
// 密钥一旦生成绝不覆盖；损坏一律 fail-fast，避免数据库密文不可恢复。
package datadir

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

const (
	dirName       = ".langhuan-data"
	credKeyFile   = "credential.key"
	keyLen        = 32
	credWaitRetry = 20
	credWaitSleep = 50 * time.Millisecond
)

// HomeResolver 返回用户主目录，便于测试注入。
type HomeResolver func() (string, error)

// Dir 描述已解析的数据目录。
type Dir struct{ path string }

// Resolve 通过 HomeResolver 解析主目录并构造 ~/.langhuan-data 路径。
// 无法解析或主目录为空时返回可行动错误，不回退工作目录或临时目录。
func Resolve(home HomeResolver) (Dir, error) {
	if home == nil {
		home = os.UserHomeDir
	}
	h, err := home()
	if err != nil {
		return Dir{}, fmt.Errorf("解析用户主目录失败: %w", err)
	}
	if h == "" {
		return Dir{}, errors.New("解析用户主目录失败: 主目录为空")
	}
	return Dir{path: filepath.Join(h, dirName)}, nil
}

// New 构造指向已存在路径的 Dir，主要供测试使用。生产路径应走 Resolve。
func New(path string) Dir { return Dir{path: path} }

// Path 返回数据目录绝对路径。
func (d Dir) Path() string { return d.path }

// CredentialKeyPath 返回 credential.key 的完整路径。
func (d Dir) CredentialKeyPath() string { return filepath.Join(d.path, credKeyFile) }

// Ensure 创建数据根目录（0700）。已有目录权限过宽时尝试收紧，失败则拒绝。
// Windows 没有等价 POSIX mode，权限相关操作为 no-op，仅保证目录存在。
func (d Dir) Ensure() error {
	info, err := os.Stat(d.path)
	if err == nil {
		if !isWindows() && info.Mode().Perm() != 0o700 {
			if cerr := os.Chmod(d.path, 0o700); cerr != nil {
				return fmt.Errorf("收紧数据目录权限失败（当前 %o）: %w", info.Mode().Perm(), cerr)
			}
		}
		return nil
	}
	if !os.IsNotExist(err) {
		return fmt.Errorf("检查数据目录失败: %w", err)
	}
	// MkdirAll 创建 .langhuan-data 及缺失的父级（如 HOME 尚未创建），全部 0700。
	if err := os.MkdirAll(d.path, 0o700); err != nil {
		return fmt.Errorf("创建数据目录失败: %w", err)
	}
	return nil
}

// EnsureCredentialKey 保证 credential.key 存在且有效，返回解码后的 32 字节主密钥：
//   - 不存在 → O_CREATE|O_EXCL 生成 keyLen 个随机字节的 Base64 文本，0600，fsync
//   - 已存在 → 读取并校验（Base64、32 字节、权限收紧），绝不覆盖
//
// 损坏文件一律 fail-fast，绝不自动轮换（数据库密文依赖该密钥）。
// 并发首次启动时，创建失败的一方遇到 EEXIST 后做有界重读，直到胜出进程写完。
func (d Dir) EnsureCredentialKey() ([]byte, error) {
	path := d.CredentialKeyPath()
	if raw, err := os.ReadFile(path); err == nil {
		// 文件存在但内容为空：可能是并发首次启动时另一进程刚 O_EXCL 创建、
		// 尚未写完。此时不能当作损坏 fail-fast，进入有界重试等待写入完成。
		if len(trimSpaceBytes(raw)) == 0 {
			return readCredentialWithRetry(path)
		}
		return validateCredentialBytes(raw, path)
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("读取凭证密钥失败: %w", err)
	}

	key := make([]byte, keyLen)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("生成凭证密钥失败: %w", err)
	}
	b64 := []byte(base64.StdEncoding.EncodeToString(key))
	if err := writeFileExclusive(path, b64, 0o600); err != nil {
		if errors.Is(err, os.ErrExist) {
			return readCredentialWithRetry(path)
		}
		return nil, fmt.Errorf("写入凭证密钥失败: %w", err)
	}
	return key, nil
}

// writeFileExclusive 以 O_CREATE|O_EXCL 创建文件并写入+fsync。
func writeFileExclusive(path string, data []byte, perm os.FileMode) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, perm)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write(data); err != nil {
		return err
	}
	return f.Sync()
}

func validateCredentialBytes(raw []byte, path string) ([]byte, error) {
	key, err := base64.StdEncoding.DecodeString(string(trimSpaceBytes(raw)))
	if err != nil || len(key) != keyLen {
		return nil, fmt.Errorf("凭证密钥文件损坏（非 Base64 或长度非 32 字节）: %s", path)
	}
	if !isWindows() {
		if info, statErr := os.Stat(path); statErr == nil && info.Mode().Perm() != 0o600 {
			_ = os.Chmod(path, 0o600)
		}
	}
	return key, nil
}

func readCredentialWithRetry(path string) ([]byte, error) {
	for i := 0; i < credWaitRetry; i++ {
		if raw, err := os.ReadFile(path); err == nil && len(trimSpaceBytes(raw)) > 0 {
			return validateCredentialBytes(raw, path)
		}
		time.Sleep(credWaitSleep)
	}
	return nil, fmt.Errorf("等待凭证密钥写入超时: %s", path)
}

func trimSpaceBytes(b []byte) []byte {
	start, end := 0, len(b)
	for start < end && isSpaceByte(b[start]) {
		start++
	}
	for end > start && isSpaceByte(b[end-1]) {
		end--
	}
	return b[start:end]
}

func isSpaceByte(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}

func isWindows() bool { return runtime.GOOS == "windows" }
