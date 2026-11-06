package team

import (
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// The enum tags on snapshots must list exactly the values the domain uses,
// or the clients generated from the API's description refuse real answers.
func TestSnapshotEnumTagsMatchTheDomain(t *testing.T) {
	tag := func(field string) []string {
		f, _ := reflect.TypeOf(TeamMemberSnapshot{}).FieldByName(field)
		return strings.Split(f.Tag.Get("enum"), ",")
	}
	assert.ElementsMatch(t, []string{string(RoleLead), string(RoleMember)}, tag("Role"))
	assert.ElementsMatch(t, []string{string(MemberUser), string(MemberAgent)}, tag("PrincipalKind"))
}
