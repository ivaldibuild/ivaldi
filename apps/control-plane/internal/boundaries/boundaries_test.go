package boundaries

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const modulePath = "github.com/ivaldibuild/ivaldi-control-plane"

// moduleRoot is apps/control-plane, two directories above this file.
func moduleRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok)
	return filepath.Join(filepath.Dir(file), "..", "..")
}

func TestModuleKeepsComponentsApart(t *testing.T) {
	violations, err := Check(moduleRoot(t), modulePath)
	require.NoError(t, err)
	for _, v := range violations {
		t.Error(v)
	}
}

func TestCheckFindsEveryKindOfViolation(t *testing.T) {
	violations, err := Check(filepath.Join("testdata", "module"), "example.com/m")
	require.NoError(t, err)

	got := map[string]string{}
	for _, v := range violations {
		got[v.File+" -> "+v.Import] = v.Rule
	}
	assert.Equal(t, map[string]string{
		"internal/worker/run.go -> example.com/m/internal/delivery/postgres":   "a component must not import the control plane; share code through pkg/",
		"internal/worker/run_test.go -> example.com/m/internal/config":         "a component must not import the control plane; share code through pkg/",
		"cmd/gateway/root.go -> example.com/m/internal/indexer":                "components must not import each other; share code through pkg/",
		"internal/delivery/service.go -> example.com/m/internal/gateway/relay": "the control plane must not import a component",
		"cmd/server/root.go -> example.com/m/cmd/worker":                       "the control plane must not import a component",
	}, got, "allowed imports, such as the root command's and a component's own, are not reported")
}
