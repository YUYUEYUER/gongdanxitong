package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCollectWidgetLogoutTokensRevokesEverySource(t *testing.T) {
	t.Parallel()

	tokens, err := collectWidgetLogoutTokens("cookie-session", "Bearer header-session", "cookie-visitor", "header-visitor")
	require.NoError(t, err)
	require.Equal(t, []string{"cookie-session", "header-session", "cookie-visitor", "header-visitor"}, tokens)
}

func TestCollectWidgetLogoutTokensDeduplicatesAndRejectsMalformedHeaders(t *testing.T) {
	t.Parallel()

	tokens, err := collectWidgetLogoutTokens("same-token", "bearer same-token", "same-token", "")
	require.NoError(t, err)
	require.Equal(t, []string{"same-token"}, tokens)

	_, err = collectWidgetLogoutTokens("", "Basic credential", "", "")
	require.Error(t, err)
	_, err = collectWidgetLogoutTokens("", "Bearer "+strings.Repeat("x", 257), "", "")
	require.Error(t, err)
	_, err = collectWidgetLogoutTokens("", "", "", "token with spaces")
	require.Error(t, err)
}
