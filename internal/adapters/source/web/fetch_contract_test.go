package web

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"

	sourceport "github.com/dajee/langhuan/internal/ports/source"
)

// 本文件覆盖引擎合同的分类判定（spec §6.5），与 fetch_http.go 的抓取行为测试分离。

func TestClassifyStatus(t *testing.T) {
	t.Parallel()

	cases := []struct {
		status int
		want   PageOutcome
	}{
		{status: 0, want: OutcomeRetryable},
		{status: http.StatusOK, want: OutcomeOK},
		{status: http.StatusNoContent, want: OutcomeOK},
		{status: http.StatusNotModified, want: OutcomeOK},
		{status: http.StatusMovedPermanently, want: OutcomeOK},
		{status: http.StatusNotFound, want: OutcomeGone},
		{status: http.StatusGone, want: OutcomeGone},
		{status: http.StatusUnauthorized, want: OutcomeBlocked},
		{status: http.StatusForbidden, want: OutcomeBlocked},
		{status: http.StatusBadRequest, want: OutcomeSkipped},
		{status: http.StatusMethodNotAllowed, want: OutcomeSkipped},
		{status: http.StatusUnprocessableEntity, want: OutcomeSkipped},
		{status: http.StatusRequestTimeout, want: OutcomeRetryable},
		{status: http.StatusTooManyRequests, want: OutcomeRetryable},
		{status: http.StatusInternalServerError, want: OutcomeRetryable},
		{status: http.StatusBadGateway, want: OutcomeRetryable},
	}
	for _, tc := range cases {
		t.Run(strconv.Itoa(tc.status), func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, ClassifyStatus(tc.status))
		})
	}
}

func TestClassifyError(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  error
		want PageOutcome
	}{
		{name: "nil", err: nil, want: OutcomeOK},
		{name: "内容超限", err: sourceport.ErrSourceContentTooLarge, want: OutcomeOversize},
		{name: "源不可用", err: sourceport.ErrSourceUnavailable, want: OutcomeRetryable},
		{name: "对象不存在", err: sourceport.ErrSourceNotFound, want: OutcomeGone},
		{name: "超时", err: context.DeadlineExceeded, want: OutcomeRetryable},
		{name: "取消", err: context.Canceled, want: OutcomeRetryable},
		{name: "包装后的页面错误", err: &PageError{Outcome: OutcomeBlocked, Err: errors.New("x")}, want: OutcomeBlocked},
		{name: "未分类错误", err: errors.New("boom"), want: OutcomeRetryable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, ClassifyError(tc.err))
		})
	}
}
