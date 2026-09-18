package forge

import (
	"bytes"
	"fmt"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSecretPrintsMasked covers every way a token can reach a page: fmt's verbs, a struct
// holding one, and slog under both handlers, each of which shows the last three characters
// and nothing else.
func TestSecretPrintsMasked(t *testing.T) {
	t.Parallel()

	secret := NewSecret("ghp_abcdefghijklmnopqrstuvwxyz0123456789fba\n")
	const want = "*****fba"

	assert.Equal(t, want, secret.String())
	assert.Equal(t, want, fmt.Sprint(secret))
	assert.Equal(t, want, fmt.Sprintf("%v", secret))
	assert.Equal(t, want, fmt.Sprintf("%#v", secret))
	assert.Equal(t, "{Authorization "+want+" 0001-01-01 00:00:00 +0000 UTC}", fmt.Sprintf("%v", Credential{Header: HeaderBearer, Token: secret}))
	assert.NotContains(t, fmt.Sprintf("%+v", Credential{Token: secret}), "ghp_")

	const key = "token"

	var text bytes.Buffer
	slog.New(slog.NewTextHandler(&text, nil)).LogAttrs(t.Context(), slog.LevelInfo, "connected", slog.Any(key, secret))
	assert.Contains(t, text.String(), "token="+want)
	assert.NotContains(t, text.String(), "ghp_")

	var jsonOut bytes.Buffer
	slog.New(slog.NewJSONHandler(&jsonOut, nil)).LogAttrs(t.Context(), slog.LevelInfo, "connected", slog.Any(key, secret))
	assert.Contains(t, jsonOut.String(), `"token":"`+want+`"`)
	assert.NotContains(t, jsonOut.String(), "ghp_")

	assert.Equal(t, "ghp_abcdefghijklmnopqrstuvwxyz0123456789fba", secret.Expose())
}

// TestSecretShortAndEmpty covers a value too short to show a tail of, and none at all.
func TestSecretShortAndEmpty(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "*****", NewSecret("short").String())
	assert.Equal(t, "*****", NewSecret("  ").String())
	assert.True(t, NewSecret("  ").IsZero())
	assert.False(t, NewSecret("x").IsZero())
	require.Equal(t, "*****", Secret{}.String())
}
