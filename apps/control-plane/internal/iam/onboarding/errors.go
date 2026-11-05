package onboarding

type ErrOrganizationConflict struct {
	Reason string
}

func (e *ErrOrganizationConflict) Error() string {
	return "organization conflict: " + e.Reason
}

func NewErrOrganizationConflict(reason string) *ErrOrganizationConflict {
	return &ErrOrganizationConflict{
		Reason: reason,
	}
}

type ErrAccountConflict struct {
	Reason string
}

func (e *ErrAccountConflict) Error() string {
	return "account conflict: " + e.Reason
}

func NewErrAccountConflict(reason string) *ErrAccountConflict {
	return &ErrAccountConflict{
		Reason: reason,
	}
}
