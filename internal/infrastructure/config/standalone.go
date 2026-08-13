// Package config 内的 standalone 子能力：零配置 standalone 模式的内建 profile 与落盘。
//
// standalone profile 在用户未提供任何 config 时由首次启动生成，落盘为
// ~/.langhuan-data/config.yaml，使用户重启后拿到一份可编辑的配置入口。
// 凭证主密钥始终是独立的 credential.key，config 仅通过 encryption_key_file 指向它，
// 密钥内容从不进入 config 文本。
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const standaloneHeader = `# 由 langhuan 首次启动自动生成（standalone 零配置模式）。
# 你可以直接编辑本文件并重启生效。删除本文件后下次启动会重建，
# 但同目录的 credential.key 绝不会重建——它是数据库密文的唯一主密钥，
# 丢失将导致已加密的 Provider 凭证 / API Key 永久不可恢复。
`

// MaterializeStandalone 在 dataDirPath 中生成 standalone profile 的 config.yaml，
// 并通过 keyEnsure 保证 credential.key 存在（已存在则复用，绝不覆盖）。
// 返回落盘 config.yaml 的绝对路径。
//
// config.yaml 已存在时直接返回路径（不覆盖，spec §2.1 第 3 层享有正式配置的严格性）。
//
// keyEnsure 是 datadir.Dir.EnsureCredentialKey 的注入点，避免本包反向依赖 datadir。
func MaterializeStandalone(dataDirPath string, keyEnsure func() ([]byte, error)) (string, error) {
	cfgPath := filepath.Join(dataDirPath, "config.yaml")
	if _, err := os.Stat(cfgPath); err == nil {
		return cfgPath, nil
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("检查 standalone 配置失败: %w", err)
	}

	if _, err := keyEnsure(); err != nil {
		return "", fmt.Errorf("准备凭证主密钥失败: %w", err)
	}

	prof := StandaloneProfile(dataDirPath)
	out, err := yaml.Marshal(prof)
	if err != nil {
		return "", fmt.Errorf("序列化 standalone 配置失败: %w", err)
	}
	body := append([]byte(standaloneHeader), out...)
	if err := writeFileExclusiveConfig(cfgPath, body, 0o600); err != nil {
		if os.IsExist(err) {
			return cfgPath, nil // 并发首次：另一进程已生成
		}
		return "", fmt.Errorf("写入 standalone 配置失败: %w", err)
	}
	return cfgPath, nil
}

// StandaloneProfile 构造零配置 standalone 模式的内建配置。
// 基于 defaultConfig 的安全默认值，覆盖数据/队列/认证相关字段为单机形态。
func StandaloneProfile(dataDirPath string) Config {
	c := defaultConfig()
	c.Server = ServerConfig{
		HTTPAddr:  "127.0.0.1:8080",
		BaseURL:   "http://127.0.0.1:8080",
		RunHTTP:   true,
		RunWorker: true,
	}
	c.Database = DatabaseConfig{
		Driver:      "sqlite",
		DSN:         "file:" + filepath.ToSlash(filepath.Join(dataDirPath, "langhuan.db")) + "?cache=shared",
		AutoMigrate: true,
	}
	c.Redis = RedisConfig{Enabled: false}
	c.Storage = StorageConfig{
		Driver:         "local",
		RawDocumentDir: filepath.Join(dataDirPath, "raw-documents"),
		Assets:         c.Storage.Assets, // 保留资产默认限制
	}
	c.Auth.Session.SecureCookie = false // localhost 明文 HTTP 必须关闭
	c.Auth.Password.Enabled = true
	c.Auth.OIDC.Enabled = false
	// 密钥与配置分离：只指向独立文件，绝不内联密钥内容
	c.Credentials = CredentialsConfig{
		EncryptionKeyFile: filepath.Join(dataDirPath, "credential.key"),
	}
	return c
}

// writeFileExclusiveConfig 以 O_CREATE|O_EXCL 创建并写入配置文件。
// 与 datadir 的 writeFileExclusive 同语义，此处独立维护以避免 config 反向依赖 datadir。
func writeFileExclusiveConfig(path string, data []byte, perm os.FileMode) error {
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
