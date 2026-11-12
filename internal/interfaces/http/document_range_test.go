package http

import (
	"testing"
)

func TestParseByteRange(t *testing.T) {
	const size = 1000
	cases := []struct {
		name      string
		header    string
		wantStart int
		wantEnd   int
		wantOK    bool
	}{
		{name: "完整区间", header: "bytes=0-999", wantStart: 0, wantEnd: 999, wantOK: true},
		{name: "部分区间", header: "bytes=100-199", wantStart: 100, wantEnd: 199, wantOK: true},
		{name: "end 超出夹紧", header: "bytes=500-9999", wantStart: 500, wantEnd: 999, wantOK: true},
		{name: "省略 end", header: "bytes=500-", wantStart: 500, wantEnd: 999, wantOK: true},
		{name: "多区间拒绝", header: "bytes=0-1,5-9", wantOK: false},
		{name: "非法格式", header: "items=0-1", wantOK: false},
		{name: "start 超界", header: "bytes=1000-", wantOK: false},
		{name: "负 start", header: "bytes=-100", wantOK: false},
		{name: "end 小于 start", header: "bytes=500-100", wantOK: false},
		{name: "空 size", header: "bytes=0-", wantOK: false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			sizeForCase := int64(size)
			if testCase.name == "空 size" {
				sizeForCase = 0
			}
			start, end, ok := parseByteRange(testCase.header, sizeForCase)
			if ok != testCase.wantOK {
				t.Fatalf("ok = %v, want %v", ok, testCase.wantOK)
			}
			if ok && (start != testCase.wantStart || end != testCase.wantEnd) {
				t.Fatalf("range = %d-%d, want %d-%d", start, end, testCase.wantStart, testCase.wantEnd)
			}
		})
	}
}

func TestETagMatch(t *testing.T) {
	const etag = `"abc123"`
	cases := []struct {
		header string
		want   bool
	}{
		{header: `"abc123"`, want: true},
		{header: `W/"abc123"`, want: true},
		{header: `*`, want: true},
		{header: `"other", "abc123"`, want: true},
		{header: `"other"`, want: false},
		{header: "", want: false},
	}
	for _, testCase := range cases {
		if got := etagMatch(testCase.header, etag); got != testCase.want {
			t.Fatalf("etagMatch(%q) = %v, want %v", testCase.header, got, testCase.want)
		}
	}
}
