//go:build integration

package testsupport

import (
	"context"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const redisImage = "redis:7-alpine"

// NewIsolatedRedis starts a disposable Redis container for one integration
// test run and returns its host:port endpoint.
func NewIsolatedRedis(t testing.TB) string {
	t.Helper()
	ctx := context.Background()
	container, err := testcontainers.Run(
		ctx,
		redisImage,
		testcontainers.WithExposedPorts("6379/tcp"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("Ready to accept connections").WithStartupTimeout(30*time.Second),
		),
	)
	if err != nil {
		t.Fatalf("启动测试 Redis 容器失败: %v", err)
	}
	t.Cleanup(func() {
		if err := testcontainers.TerminateContainer(container); err != nil {
			t.Errorf("终止测试 Redis 容器失败: %v", err)
		}
	})

	endpoint, err := container.Endpoint(ctx, "")
	if err != nil {
		t.Fatalf("获取 Redis 容器地址失败: %v", err)
	}
	return endpoint
}
