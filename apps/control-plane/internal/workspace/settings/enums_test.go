package settings

import (
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/workspace"
)

// The Level's enum tag must list exactly the levels, or the clients
// generated from the API's description refuse real answers.
func TestResolvedLevelEnumMatchesTheLevels(t *testing.T) {
	f, _ := reflect.TypeOf(Resolved{}).FieldByName("Level")
	assert.ElementsMatch(t, []string{string(workspace.LevelOrganization), string(workspace.LevelWorkspace), string(workspace.LevelProject)}, strings.Split(f.Tag.Get("enum"), ","))
}
