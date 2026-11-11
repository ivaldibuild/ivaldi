package principals

import (
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
)

// The enum tags must list exactly the values the domain uses, or the clients
// generated from the API's description refuse real answers.
func TestNamedPrincipalEnumTagsMatchTheDomain(t *testing.T) {
	tag := func(field string) []string {
		f, _ := reflect.TypeOf(NamedPrincipal{}).FieldByName(field)
		return strings.Split(f.Tag.Get("enum"), ",")
	}
	assert.ElementsMatch(t, []string{
		string(iam.PrincipalUser), string(iam.PrincipalServiceAccount), string(iam.PrincipalAgent),
		string(iam.PrincipalAgentSession), string(iam.PrincipalWorkload),
	}, tag("Kind"))
	assert.ElementsMatch(t, []string{
		string(iam.PrincipalActive), string(iam.PrincipalSuspended), string(iam.PrincipalDeactivated),
	}, tag("Status"))
}
