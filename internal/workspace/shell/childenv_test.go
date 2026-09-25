package shell

import (
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestChildEnv_ExcludesPlatformVariables(t *testing.T) {
	t.Setenv("SHERLOCK_TEST_PLATFORM_SECRET", "hunter2")

	env := childEnv()

	for _, entry := range env {
		assert.False(t, strings.HasPrefix(entry, "SHERLOCK_TEST_PLATFORM_SECRET="),
			"platform variables must not reach tool children")
	}
}

func TestChildEnv_MinimalBasics(t *testing.T) {
	env := childEnv()

	keys := make(map[string]string, len(env))
	for _, entry := range env {
		k, v, _ := strings.Cut(entry, "=")
		keys[k] = v
	}
	assert.Equal(t, []string{"HOME", "LANG", "PATH"}, slices.Sorted(maps.Keys(keys)),
		"the child env is exactly the minimal set")
	for k, v := range keys {
		assert.NotEmpty(t, v, "%s must always have a value", k)
	}
}
