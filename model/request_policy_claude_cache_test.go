package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The routing policy page saves through the request policy snapshot, so the
// Claude cache-only affinity fields must be accepted and validated there.
func TestRequestPolicyClaudeCacheAffinityOptions(t *testing.T) {
	snapshot, err := BuildRequestPolicy(map[string]string{
		"channel_affinity_setting.claude_cache_only":        "true",
		"channel_affinity_setting.claude_cache_model_regex": "^claude-(opus|sonnet)-.*$",
	})
	require.NoError(t, err)
	assert.True(t, snapshot.Affinity.ClaudeCacheOnly)
	assert.Equal(t, "^claude-(opus|sonnet)-.*$", snapshot.Affinity.ClaudeCacheModelRegex)

	defaults, err := BuildRequestPolicy(nil)
	require.NoError(t, err)
	assert.False(t, defaults.Affinity.ClaudeCacheOnly)
	assert.Equal(t, "^claude-.*$", defaults.Affinity.ClaudeCacheModelRegex)

	for _, invalid := range []string{"", "   ", "^claude-(.*$"} {
		_, err := BuildRequestPolicy(map[string]string{"channel_affinity_setting.claude_cache_model_regex": invalid})
		assert.Error(t, err, "regex %q must be rejected", invalid)
	}
	_, err = BuildRequestPolicy(map[string]string{"channel_affinity_setting.claude_cache_only": "yes"})
	assert.Error(t, err, "a non-boolean switch must be rejected")
}
