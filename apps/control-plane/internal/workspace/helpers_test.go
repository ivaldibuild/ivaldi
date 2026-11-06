package workspace

import "fmt"

const (
	testOrg  = "org_01ARZ3NDEKTSV4RRFFQ69G5FAV"
	testTeam = "tem_1"
)

// rebuildSetting returns the stored Setting s describes, as a repository does.
func rebuildSetting(s SettingSnapshot) *Setting {
	setting, err := NewSetting(NewSettingProps{
		Scope:                s.Scope,
		Key:                  s.Key,
		Value:                s.Value,
		UpdatedByPrincipalID: s.UpdatedByPrincipalID,
		PolicyDecisionID:     s.PolicyDecisionID,
		Version:              s.Version,
		CreatedAt:            &s.CreatedAt,
		UpdatedAt:            &s.UpdatedAt,
	})
	if err != nil {
		panic(fmt.Sprintf("rebuild setting: %v", err))
	}
	return setting
}
