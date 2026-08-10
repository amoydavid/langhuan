package service

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSearchQueryHashUsesTrimmedUTF8Query(t *testing.T) {
	got := searchQueryHash(" 退款政策 ")
	sum := sha256.Sum256([]byte("退款政策"))
	require.Equal(t, "sha256:v1:"+hex.EncodeToString(sum[:]), got)
}

func TestSearchQueryHashIsStable(t *testing.T) {
	require.Equal(t, searchQueryHash("退款政策"), searchQueryHash("  退款政策  "))
}

func TestCanonicalSearchQueryTrimsWhitespace(t *testing.T) {
	require.Equal(t, "退款政策", canonicalSearchQuery("  退款政策\n"))
}
