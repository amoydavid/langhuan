package main

import (
	"testing"
	"time"

	hibikenasynq "github.com/hibiken/asynq"
)

func TestRetryDelayFunc(t *testing.T) {
	min := 30 * time.Second
	max := time.Hour
	fn := retryDelayFunc(min, max)

	tests := []struct {
		name    string
		n       int
		wantMax time.Duration // 期望 delay <= 此值（封顶）
		wantMin time.Duration // 期望 delay >= 此值
	}{
		{name: "n=0 returns min", n: 0, wantMin: min, wantMax: min},
		{name: "n=1 double", n: 1, wantMin: 60 * time.Second, wantMax: 60 * time.Second},
		{name: "n=2 quadruple", n: 2, wantMin: 120 * time.Second, wantMax: 120 * time.Second},
		{name: "n=3 capped near max", n: 3, wantMin: 240 * time.Second, wantMax: max},
		{name: "n=10 capped at max", n: 10, wantMin: max, wantMax: max},
		{name: "n=40 capped at max (overflow guard)", n: 40, wantMin: max, wantMax: max},
		{name: "negative returns min", n: -1, wantMin: min, wantMax: min},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			delay := fn(tt.n, nil, &hibikenasynq.Task{})
			if delay < tt.wantMin || delay > tt.wantMax {
				t.Fatalf("delay for n=%d = %v, want [%v, %v]", tt.n, delay, tt.wantMin, tt.wantMax)
			}
		})
	}
}

func TestRetryDelayFuncMaxBackoffRespected(t *testing.T) {
	// 即使 n 很大，delay 永远不超过 max。
	min := 1 * time.Second
	max := 5 * time.Second
	fn := retryDelayFunc(min, max)
	for n := 0; n <= 50; n++ {
		delay := fn(n, nil, &hibikenasynq.Task{})
		if delay > max {
			t.Fatalf("n=%d delay=%v exceeds max=%v", n, delay, max)
		}
	}
}
