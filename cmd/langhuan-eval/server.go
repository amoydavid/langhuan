package main

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/dajee/langhuan/internal/infrastructure/config"
	"github.com/dajee/langhuan/internal/infrastructure/datadir"
)

// standaloneServer 以子进程方式拉起一个临时琅嬛实例（SQLite 单机模式），
// 评测结束即销毁；顺带持续验证单二进制交付链路（spec §7.2）。
type standaloneServer struct {
	command *exec.Cmd
	baseURL string
	dataDir string
	logFile *os.File
}

func startStandaloneServer(cfg evalConfig, repoRoot string) (*standaloneServer, error) {
	port, err := freePort()
	if err != nil {
		return nil, err
	}
	runID := fmt.Sprintf("%d", time.Now().UnixNano())
	dataDir := filepath.Join(repoRoot, ".eval-data", "runtime", runID)
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, err
	}

	// 复用琅嬛内建 standalone profile 生成配置，仅覆盖监听地址与 base_url，
	// 保证字段与主程序 config 解析始终一致。credential.key 需显式生成——
	// 正常零配置路径由 MaterializeStandalone 自动创建，直接用 profile 时没有。
	if err := datadir.New(dataDir).Ensure(); err != nil {
		return nil, fmt.Errorf("初始化运行目录失败: %w", err)
	}
	if _, err := datadir.New(dataDir).EnsureCredentialKey(); err != nil {
		return nil, fmt.Errorf("生成凭证密钥失败: %w", err)
	}
	profile := config.StandaloneProfile(dataDir)
	profile.Server.HTTPAddr = fmt.Sprintf("127.0.0.1:%d", port)
	profile.Server.BaseURL = fmt.Sprintf("http://127.0.0.1:%d", port)
	body, err := yaml.Marshal(profile)
	if err != nil {
		return nil, err
	}
	configPath := filepath.Join(dataDir, "config.yaml")
	if err := os.WriteFile(configPath, body, 0o600); err != nil {
		return nil, err
	}

	binary := cfg.Server.Binary
	if binary == "" {
		binary = filepath.Join(dataDir, "langhuan")
		fmt.Println("  构建被测二进制：go build ./cmd/langhuan …")
		build := exec.Command("go", "build", "-o", binary, "./cmd/langhuan")
		build.Dir = repoRoot
		if output, err := build.CombinedOutput(); err != nil {
			return nil, fmt.Errorf("构建 langhuan 失败: %v\n%s", err, output)
		}
	}

	logFile, err := os.Create(filepath.Join(dataDir, "server.log"))
	if err != nil {
		return nil, err
	}
	command := exec.Command(binary, "-config", configPath)
	command.Stdout = logFile
	command.Stderr = logFile
	if err := command.Start(); err != nil {
		logFile.Close()
		return nil, fmt.Errorf("启动琅嬛失败: %w", err)
	}
	server := &standaloneServer{
		command: command, baseURL: profile.Server.BaseURL, dataDir: dataDir, logFile: logFile,
	}
	if err := server.waitHealthy(90 * time.Second); err != nil {
		server.stop()
		return nil, err
	}
	return server, nil
}

func (s *standaloneServer) waitHealthy(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 2 * time.Second}
	for time.Now().Before(deadline) {
		if s.command.ProcessState != nil {
			return fmt.Errorf("琅嬛进程提前退出，日志见 %s", filepath.Join(s.dataDir, "server.log"))
		}
		response, err := client.Get(s.baseURL + "/api/v1/healthz")
		if err == nil {
			response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(300 * time.Millisecond)
	}
	return fmt.Errorf("琅嬛在 %s 内未就绪，日志见 %s", timeout, filepath.Join(s.dataDir, "server.log"))
}

func (s *standaloneServer) stop() {
	if s.command.Process != nil {
		_ = s.command.Process.Signal(os.Interrupt)
		done := make(chan struct{})
		go func() { _ = s.command.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			_ = s.command.Process.Kill()
			<-done
		}
	}
	s.logFile.Close()
}

func freePort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port, nil
}

// findRepoRoot 从工作目录向上查找 go.mod，定位琅嬛仓库根目录；
// langhuan-eval 需要在仓库内执行 go build 与读取 .eval-data。
func findRepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if info, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil && !info.IsDir() {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("未找到仓库根目录（缺少 go.mod），请在琅嬛仓库内运行")
		}
		dir = parent
	}
}

func repoHead(root string) string {
	output, err := exec.Command("git", "-C", root, "rev-parse", "HEAD").Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(output))
}
