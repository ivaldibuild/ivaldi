package iam

import (
	"encoding/base64"
	"encoding/json"
	"maps"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi/pkg/aggregate"
	"github.com/ivaldibuild/ivaldi/pkg/clock"
	"github.com/ivaldibuild/ivaldi/pkg/id"
)

// eventCase is a change to a domain object and the event it should record,
// built from the object after the change. A nil want means the change
// should record nothing.
type eventCase[T any] struct {
	name   string
	change func(t *testing.T, x T)
	want   func(x T) aggregate.Event
}

type eventRecorder interface {
	PullEvents() []aggregate.Event
}

// checkEvents runs each case on a fresh object whose creation event has
// already been pulled.
func checkEvents[T eventRecorder](t *testing.T, build func(*testing.T) T, cases []eventCase[T]) {
	t.Helper()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			x := build(t)
			x.PullEvents()

			c.change(t, x)

			got := x.PullEvents()
			if c.want == nil {
				assert.Empty(t, got, "nothing changed")
				return
			}
			assert.Equal(t, []aggregate.Event{c.want(x)}, got)
		})
	}
}

// mustBuild returns v, panicking on err: fixtures are valid by construction.
func mustBuild[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

func serviceAccountNew(t *testing.T) *ServiceAccount {
	return mustBuild(NewServiceAccount(principalServiceAccountProps(), nil))
}

func workloadNew(t *testing.T) *Workload {
	return mustBuild(NewWorkload(principalWorkloadProps(), nil))
}

func xidNew(t *testing.T) *ExternalIdentity { return mustBuild(NewExternalIdentity(xidProps(), nil)) }

func aidNew(t *testing.T) *AccountIdentity { return mustBuild(NewAccountIdentity(aidProps(), nil)) }

func groupMemberNew(t *testing.T) *GroupMember { return mustBuild(NewGroupMember(groupMemberProps())) }

func clientNew(t *testing.T) *OAuthClient {
	return mustBuild(NewOAuthClient(clientDynamicProps(), nil))
}

func passwordNew(t *testing.T) *PasswordCredential {
	return mustBuild(NewPasswordCredential(NewPasswordCredentialProps{AccountID: testAccount, PasswordHash: pwHash}))
}

func TestNewRecordsCreatedOnlyForNewObjects(t *testing.T) {
	clock.Fix(t, t0)
	tests := []struct {
		name string
		// created returns a new object's events and the one it should have.
		created func(t *testing.T) ([]aggregate.Event, aggregate.Event)
		// stored returns the events of an object rebuilt from storage.
		stored func(t *testing.T) []aggregate.Event
	}{
		{"organization",
			func(t *testing.T) ([]aggregate.Event, aggregate.Event) {
				o := orgNew(t)
				return o.PullEvents(), OrganizationCreated{o.Snapshot()}
			},
			func(t *testing.T) []aggregate.Event {
				return mustBuild(NewOrganization(orgProps(), new(id.New("org")))).PullEvents()
			}},
		{"account",
			func(t *testing.T) ([]aggregate.Event, aggregate.Event) {
				a := accountNew(t)
				return a.PullEvents(), AccountCreated{a.Snapshot()}
			},
			func(t *testing.T) []aggregate.Event {
				return mustBuild(NewAccount(accountProps(), new(id.New("acc")))).PullEvents()
			}},
		{"user",
			func(t *testing.T) ([]aggregate.Event, aggregate.Event) {
				u := principalNewUser(t)
				return u.PullEvents(), UserCreated{u.Snapshot()}
			},
			func(t *testing.T) []aggregate.Event {
				return mustBuild(NewUser(principalUserProps(), new(id.New("prn")))).PullEvents()
			}},
		{"service account",
			func(t *testing.T) ([]aggregate.Event, aggregate.Event) {
				s := serviceAccountNew(t)
				return s.PullEvents(), ServiceAccountCreated{s.Snapshot()}
			},
			func(t *testing.T) []aggregate.Event {
				return mustBuild(NewServiceAccount(principalServiceAccountProps(), new(id.New("prn")))).PullEvents()
			}},
		{"agent",
			func(t *testing.T) ([]aggregate.Event, aggregate.Event) {
				a := agentNew(t)
				return a.PullEvents(), AgentCreated{a.Snapshot()}
			},
			func(t *testing.T) []aggregate.Event {
				return mustBuild(NewAgent(agentProps(), new(id.New("prn")))).PullEvents()
			}},
		{"agent session",
			func(t *testing.T) ([]aggregate.Event, aggregate.Event) {
				s := sessionNew(t)
				return s.PullEvents(), AgentSessionCreated{s.Snapshot()}
			},
			func(t *testing.T) []aggregate.Event {
				return mustBuild(NewAgentSession(sessionProps(), new(id.New("prn")))).PullEvents()
			}},
		{"workload",
			func(t *testing.T) ([]aggregate.Event, aggregate.Event) {
				w := workloadNew(t)
				return w.PullEvents(), WorkloadCreated{w.Snapshot()}
			},
			func(t *testing.T) []aggregate.Event {
				return mustBuild(NewWorkload(principalWorkloadProps(), new(id.New("prn")))).PullEvents()
			}},
		{"api token",
			func(t *testing.T) ([]aggregate.Event, aggregate.Event) {
				tok := tokenNew(t, tokenProps())
				return tok.PullEvents(), APITokenCreated{tok.Snapshot()}
			},
			func(t *testing.T) []aggregate.Event {
				return mustBuild(NewAPIToken(tokenProps(), new(id.New("tok")))).PullEvents()
			}},
		{"external identity",
			func(t *testing.T) ([]aggregate.Event, aggregate.Event) {
				e := xidNew(t)
				return e.PullEvents(), ExternalIdentityCreated{e.Snapshot()}
			},
			func(t *testing.T) []aggregate.Event {
				return mustBuild(NewExternalIdentity(xidProps(), new(id.New("xid")))).PullEvents()
			}},
		{"account identity",
			func(t *testing.T) ([]aggregate.Event, aggregate.Event) {
				a := aidNew(t)
				return a.PullEvents(), AccountIdentityCreated{a.Snapshot()}
			},
			func(t *testing.T) []aggregate.Event {
				return mustBuild(NewAccountIdentity(aidProps(), new(id.New("aid")))).PullEvents()
			}},
		{"grant",
			func(t *testing.T) ([]aggregate.Event, aggregate.Event) {
				g := grantNew(t)
				return g.PullEvents(), GrantCreated{g.Snapshot()}
			},
			func(t *testing.T) []aggregate.Event {
				return mustBuild(NewGrant(grantProps(), new(id.New("grt")))).PullEvents()
			}},
		{"group",
			func(t *testing.T) ([]aggregate.Event, aggregate.Event) {
				g := groupNew(t)
				return g.PullEvents(), GroupCreated{g.Snapshot()}
			},
			func(t *testing.T) []aggregate.Event {
				return mustBuild(NewGroup(groupManualProps(), new(id.New("grp")))).PullEvents()
			}},
		{"group member",
			func(t *testing.T) ([]aggregate.Event, aggregate.Event) {
				m := groupMemberNew(t)
				return m.PullEvents(), GroupMemberAdded{m.Snapshot()}
			},
			func(t *testing.T) []aggregate.Event {
				props := groupMemberProps()
				props.CreatedAt = new(t0)
				return mustBuild(NewGroupMember(props)).PullEvents()
			}},
		{"identity provider",
			func(t *testing.T) ([]aggregate.Event, aggregate.Event) {
				p := idpNew(t, idpProps())
				return p.PullEvents(), IdentityProviderCreated{p.Snapshot()}
			},
			func(t *testing.T) []aggregate.Event {
				return mustBuild(NewIdentityProvider(idpProps(), new(id.New("idp")))).PullEvents()
			}},
		{"oauth client",
			func(t *testing.T) ([]aggregate.Event, aggregate.Event) {
				c := clientNew(t)
				return c.PullEvents(), OAuthClientCreated{c.Snapshot()}
			},
			func(t *testing.T) []aggregate.Event {
				return mustBuild(NewOAuthClient(clientDynamicProps(), new(id.New("ocl")))).PullEvents()
			}},
		{"oauth refresh token",
			func(t *testing.T) ([]aggregate.Event, aggregate.Event) {
				r := refreshNew(t)
				return r.PullEvents(), OAuthRefreshTokenCreated{r.Snapshot()}
			},
			func(t *testing.T) []aggregate.Event {
				return mustBuild(NewOAuthRefreshToken(refreshProps(), new(id.New("ort")))).PullEvents()
			}},
		{"organization domain",
			func(t *testing.T) ([]aggregate.Event, aggregate.Event) {
				d := orgDomainNew(t)
				return d.PullEvents(), OrganizationDomainCreated{d.Snapshot()}
			},
			func(t *testing.T) []aggregate.Event {
				props := orgDomainProps()
				props.CreatedAt = new(t0)
				return mustBuild(NewOrganizationDomain(props)).PullEvents()
			}},
		{"password credential",
			func(t *testing.T) ([]aggregate.Event, aggregate.Event) {
				c := passwordNew(t)
				return c.PullEvents(), PasswordCredentialCreated{c.Snapshot()}
			},
			func(t *testing.T) []aggregate.Event {
				return mustBuild(NewPasswordCredential(NewPasswordCredentialProps{
					AccountID: testAccount, PasswordHash: pwHash, UpdatedAt: new(t0),
				})).PullEvents()
			}},
		{"role",
			func(t *testing.T) ([]aggregate.Event, aggregate.Event) {
				r := roleCustom(t)
				return r.PullEvents(), RoleCreated{r.Snapshot()}
			},
			func(t *testing.T) []aggregate.Event {
				return roleBuiltIn(t, RoleViewer).PullEvents()
			}},
		{"signing key",
			func(t *testing.T) ([]aggregate.Event, aggregate.Event) {
				k := keyNew(t)
				return k.PullEvents(), SigningKeyCreated{k.Snapshot()}
			},
			func(t *testing.T) []aggregate.Event {
				props := keyProps()
				props.CreatedAt = new(t0)
				return mustBuild(NewSigningKey(props)).PullEvents()
			}},
		{"workload credential",
			func(t *testing.T) ([]aggregate.Event, aggregate.Event) {
				c := wcrNew(t, wcrProps())
				return c.PullEvents(), WorkloadCredentialCreated{c.Snapshot()}
			},
			func(t *testing.T) []aggregate.Event {
				return mustBuild(NewWorkloadCredential(wcrProps(), new(id.New("wcr")))).PullEvents()
			}},
		{"workload enrollment token",
			func(t *testing.T) ([]aggregate.Event, aggregate.Event) {
				tok := wetNew(t)
				return tok.PullEvents(), WorkloadEnrollmentTokenCreated{tok.Snapshot()}
			},
			func(t *testing.T) []aggregate.Event {
				return mustBuild(NewWorkloadEnrollmentToken(wetProps(), new(id.New("wet")))).PullEvents()
			}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, want := tt.created(t)
			assert.Equal(t, []aggregate.Event{want}, got)
			assert.Empty(t, tt.stored(t), "an object rebuilt from storage records nothing")
		})
	}
}

func TestOAuthAuthorizationCodeRecordsNoCreation(t *testing.T) {
	clock.Fix(t, t0)

	assert.Empty(t, codeNew(t).PullEvents(), "a code cannot tell a new one from a stored one")
}

func TestOrganizationEvents(t *testing.T) {
	clock.Fix(t, t0)
	checkEvents(t, orgNew, []eventCase[*Organization]{
		{"update", func(t *testing.T, o *Organization) {
			require.NoError(t, o.Update(UpdateOrganizationProps{DisplayName: new("Acme Co"), Metadata: map[string]string{"plan": "enterprise"}}))
		}, func(o *Organization) aggregate.Event { return OrganizationUpdated{o.Snapshot()} }},
		{"update with the same values", func(t *testing.T, o *Organization) {
			require.NoError(t, o.Update(UpdateOrganizationProps{
				Slug: new("acme"), LogoURL: new("https://acme.example/logo.png"), Metadata: map[string]string{"plan": "team"},
			}))
		}, nil},
		{"suspend", func(t *testing.T, o *Organization) { require.NoError(t, o.Suspend()) },
			func(o *Organization) aggregate.Event {
				return OrganizationStatusChanged{Organization: o.Snapshot(), PreviousStatus: OrganizationActive}
			}},
		{"suspend twice", func(t *testing.T, o *Organization) {
			require.NoError(t, o.Suspend())
			o.PullEvents()
			require.NoError(t, o.Suspend())
		}, nil},
		{"reactivate", func(t *testing.T, o *Organization) {
			require.NoError(t, o.Suspend())
			o.PullEvents()
			require.NoError(t, o.Reactivate())
		}, func(o *Organization) aggregate.Event {
			return OrganizationStatusChanged{Organization: o.Snapshot(), PreviousStatus: OrganizationSuspended}
		}},
		{"delete", func(t *testing.T, o *Organization) { o.Delete() },
			func(o *Organization) aggregate.Event {
				return OrganizationStatusChanged{Organization: o.Snapshot(), PreviousStatus: OrganizationActive}
			}},
	})
}

func TestAccountEvents(t *testing.T) {
	clock.Fix(t, t0)
	checkEvents(t, accountNew, []eventCase[*Account]{
		{"rename", func(t *testing.T, a *Account) { require.NoError(t, a.Rename("Ada King")) },
			func(a *Account) aggregate.Event { return AccountRenamed{a.Snapshot()} }},
		{"rename to the same name", func(t *testing.T, a *Account) { require.NoError(t, a.Rename("Ada Lovelace")) }, nil},
		{"change email", func(t *testing.T, a *Account) { require.NoError(t, a.ChangeEmail("ada@other.example")) },
			func(a *Account) aggregate.Event { return AccountEmailChanged{a.Snapshot()} }},
		{"change email to the same", func(t *testing.T, a *Account) { require.NoError(t, a.ChangeEmail("Ada@Acme.Example")) }, nil},
		{"verify email", func(t *testing.T, a *Account) { a.VerifyEmail() },
			func(a *Account) aggregate.Event { return AccountEmailVerified{a.Snapshot()} }},
		{"verify email twice", func(t *testing.T, a *Account) { a.VerifyEmail(); a.PullEvents(); a.VerifyEmail() }, nil},
		{"suspend", func(t *testing.T, a *Account) { require.NoError(t, a.Suspend()) },
			func(a *Account) aggregate.Event {
				return AccountStatusChanged{Account: a.Snapshot(), PreviousStatus: AccountActive}
			}},
		{"suspend twice", func(t *testing.T, a *Account) {
			require.NoError(t, a.Suspend())
			a.PullEvents()
			require.NoError(t, a.Suspend())
		}, nil},
		{"deactivate a suspended account", func(t *testing.T, a *Account) {
			require.NoError(t, a.Suspend())
			a.PullEvents()
			require.NoError(t, a.Deactivate())
		}, func(a *Account) aggregate.Event {
			return AccountStatusChanged{Account: a.Snapshot(), PreviousStatus: AccountSuspended}
		}},
	})
}

func TestPrincipalEvents(t *testing.T) {
	clock.Fix(t, t0)
	checkEvents(t, principalNewUser, []eventCase[*User]{
		{"rename", func(t *testing.T, u *User) { require.NoError(t, u.Rename("Ada King")) },
			func(u *User) aggregate.Event { return PrincipalRenamed{u.snapshot()} }},
		{"rename to the same name", func(t *testing.T, u *User) { require.NoError(t, u.Rename("Ada Lovelace")) }, nil},
		{"set metadata", func(t *testing.T, u *User) { require.NoError(t, u.SetMetadata(nil)) },
			func(u *User) aggregate.Event { return PrincipalMetadataChanged{u.snapshot()} }},
		{"set the same metadata", func(t *testing.T, u *User) {
			require.NoError(t, u.SetMetadata(map[string]string{"team": "platform"}))
		}, nil},
		{"suspend", func(t *testing.T, u *User) { require.NoError(t, u.Suspend()) },
			func(u *User) aggregate.Event {
				return PrincipalStatusChanged{Principal: u.snapshot(), PreviousStatus: PrincipalActive}
			}},
		{"suspend twice", func(t *testing.T, u *User) {
			require.NoError(t, u.Suspend())
			u.PullEvents()
			require.NoError(t, u.Suspend())
		}, nil},
		{"reactivate", func(t *testing.T, u *User) {
			require.NoError(t, u.Deactivate())
			u.PullEvents()
			require.NoError(t, u.Reactivate())
		}, func(u *User) aggregate.Event {
			return PrincipalStatusChanged{Principal: u.snapshot(), PreviousStatus: PrincipalDeactivated}
		}},
	})
}

func TestServiceAccountEvents(t *testing.T) {
	clock.Fix(t, t0)
	checkEvents(t, serviceAccountNew, []eventCase[*ServiceAccount]{
		{"set description", func(t *testing.T, s *ServiceAccount) { s.SetDescription("Releases from CI") },
			func(s *ServiceAccount) aggregate.Event { return ServiceAccountDescriptionChanged{s.Snapshot()} }},
		{"set the same description", func(t *testing.T, s *ServiceAccount) { s.SetDescription("Deploys from CI") }, nil},
	})
}

func TestAgentSessionEvents(t *testing.T) {
	clock.Fix(t, t0)
	checkEvents(t, sessionNew, []eventCase[*AgentSession]{
		{"revoke", func(t *testing.T, s *AgentSession) { s.Revoke() },
			func(s *AgentSession) aggregate.Event { return AgentSessionRevoked{s.Snapshot()} }},
		{"revoke twice", func(t *testing.T, s *AgentSession) { s.Revoke(); s.PullEvents(); s.Revoke() }, nil},
		{"reactivate a suspended session", func(t *testing.T, s *AgentSession) {
			require.NoError(t, s.Suspend())
			s.PullEvents()
			require.NoError(t, s.Reactivate())
		}, func(s *AgentSession) aggregate.Event {
			return PrincipalStatusChanged{Principal: s.snapshot(), PreviousStatus: PrincipalSuspended}
		}},
		{"reactivate an active session", func(t *testing.T, s *AgentSession) { require.NoError(t, s.Reactivate()) }, nil},
	})
}

func TestContactRecordsNoEvent(t *testing.T) {
	clock.Fix(t, t0)
	checkEvents(t, workloadNew, []eventCase[*Workload]{{"workload seen", func(t *testing.T, w *Workload) { w.Seen() }, nil}})
	checkEvents(t, func(t *testing.T) *APIToken { return tokenNew(t, tokenProps()) },
		[]eventCase[*APIToken]{{"api token used", func(t *testing.T, tok *APIToken) { tok.RecordUse() }, nil}})
	checkEvents(t, xidNew, []eventCase[*ExternalIdentity]{
		{"external identity signed in", func(t *testing.T, e *ExternalIdentity) { e.RecordLogin() }, nil},
	})
	checkEvents(t, aidNew, []eventCase[*AccountIdentity]{
		{"account identity signed in", func(t *testing.T, a *AccountIdentity) { a.RecordLogin() }, nil},
	})
	checkEvents(t, clientNew, []eventCase[*OAuthClient]{{"oauth client used", func(t *testing.T, c *OAuthClient) { c.RecordUse() }, nil}})
}

func TestAPITokenEvents(t *testing.T) {
	clock.Fix(t, t0)
	checkEvents(t, func(t *testing.T) *APIToken { return tokenNew(t, tokenProps()) }, []eventCase[*APIToken]{
		{"revoke", func(t *testing.T, tok *APIToken) { tok.Revoke() },
			func(tok *APIToken) aggregate.Event { return APITokenRevoked{tok.Snapshot()} }},
		{"revoke twice", func(t *testing.T, tok *APIToken) { tok.Revoke(); tok.PullEvents(); tok.Revoke() }, nil},
	})
}

func TestGroupEvents(t *testing.T) {
	clock.Fix(t, t0)
	checkEvents(t, groupNew, []eventCase[*Group]{
		{"rename", func(t *testing.T, g *Group) { require.NoError(t, g.Rename("Platform team")) },
			func(g *Group) aggregate.Event { return GroupRenamed{g.Snapshot()} }},
		{"rename to the same name", func(t *testing.T, g *Group) { require.NoError(t, g.Rename("Platform")) }, nil},
	})
}

func TestIdentityProviderEvents(t *testing.T) {
	clock.Fix(t, t0)
	unmanaged := func(t *testing.T) *IdentityProvider { return idpNew(t, idpProps()) }
	sameProps := func() UpdateIdentityProviderProps {
		return UpdateIdentityProviderProps{
			Name: new("Example SSO"), Config: new(idpOIDCConfig()), SecretRef: new("vault://idp/example"), JITProvisioning: new(false),
		}
	}

	checkEvents(t, unmanaged, []eventCase[*IdentityProvider]{
		{"update", func(t *testing.T, p *IdentityProvider) {
			require.NoError(t, p.Update(UpdateIdentityProviderProps{JITProvisioning: new(true)}))
		}, func(p *IdentityProvider) aggregate.Event { return IdentityProviderUpdated{p.Snapshot()} }},
		{"update with the same values", func(t *testing.T, p *IdentityProvider) { require.NoError(t, p.Update(sameProps())) }, nil},
		{"disable", func(t *testing.T, p *IdentityProvider) { require.NoError(t, p.Disable()) },
			func(p *IdentityProvider) aggregate.Event {
				return IdentityProviderStatusChanged{IdentityProvider: p.Snapshot(), PreviousStatus: IdentityProviderActive}
			}},
		{"disable twice", func(t *testing.T, p *IdentityProvider) {
			require.NoError(t, p.Disable())
			p.PullEvents()
			require.NoError(t, p.Disable())
		}, nil},
		{"enable", func(t *testing.T, p *IdentityProvider) {
			require.NoError(t, p.Disable())
			p.PullEvents()
			require.NoError(t, p.Enable())
		}, func(p *IdentityProvider) aggregate.Event {
			return IdentityProviderStatusChanged{IdentityProvider: p.Snapshot(), PreviousStatus: IdentityProviderDisabled}
		}},
		{"configure an unmanaged provider as it is", func(t *testing.T, p *IdentityProvider) {
			require.NoError(t, p.Configure(sameProps()))
		}, func(p *IdentityProvider) aggregate.Event { return IdentityProviderConfigured{p.Snapshot()} }},
	})
	checkEvents(t, idpManaged, []eventCase[*IdentityProvider]{
		{"configure with the same values", func(t *testing.T, p *IdentityProvider) { require.NoError(t, p.Configure(sameProps())) }, nil},
		{"configure with new values", func(t *testing.T, p *IdentityProvider) {
			props := sameProps()
			props.Name = new("Example SSO 2")
			require.NoError(t, p.Configure(props))
		}, func(p *IdentityProvider) aggregate.Event { return IdentityProviderConfigured{p.Snapshot()} }},
		{"unconfigure", func(t *testing.T, p *IdentityProvider) { require.NoError(t, p.Unconfigure()) },
			func(p *IdentityProvider) aggregate.Event {
				return IdentityProviderStatusChanged{IdentityProvider: p.Snapshot(), PreviousStatus: IdentityProviderActive}
			}},
	})
}

func TestOAuthClientEvents(t *testing.T) {
	clock.Fix(t, t0)
	uris := clientDynamicProps().RedirectURIs
	checkEvents(t, clientNew, []eventCase[*OAuthClient]{
		{"update", func(t *testing.T, c *OAuthClient) { require.NoError(t, c.Update("Editor 2", uris)) },
			func(c *OAuthClient) aggregate.Event { return OAuthClientUpdated{c.Snapshot()} }},
		{"update with the same values", func(t *testing.T, c *OAuthClient) { require.NoError(t, c.Update("Editor", uris)) }, nil},
	})
}

func TestOAuthAuthorizationCodeEvents(t *testing.T) {
	clock.Fix(t, t0)
	checkEvents(t, codeNew, []eventCase[*OAuthAuthorizationCode]{
		{"redeem", func(t *testing.T, c *OAuthAuthorizationCode) {
			require.NoError(t, c.Redeem(testClient, codeRedirect, codeVerifier))
		}, func(c *OAuthAuthorizationCode) aggregate.Event { return OAuthAuthorizationCodeRedeemed{c.Snapshot()} }},
		{"redeem with the wrong verifier", func(t *testing.T, c *OAuthAuthorizationCode) {
			require.Error(t, c.Redeem(testClient, codeRedirect, codeVerifier+"x"))
		}, nil},
	})
}

func TestOAuthRefreshTokenEvents(t *testing.T) {
	clock.Fix(t, t0)
	checkEvents(t, refreshNew, []eventCase[*OAuthRefreshToken]{
		{"rotate", func(t *testing.T, r *OAuthRefreshToken) {
			next, err := r.Rotate(testClient, testHash("refresh-2"), t2)
			require.NoError(t, err)
			assert.Equal(t, []aggregate.Event{OAuthRefreshTokenCreated{next.Snapshot()}}, next.PullEvents(), "the replacement is issued")
		}, func(r *OAuthRefreshToken) aggregate.Event { return OAuthRefreshTokenRotated{r.Snapshot()} }},
		{"revoke", func(t *testing.T, r *OAuthRefreshToken) { r.Revoke() },
			func(r *OAuthRefreshToken) aggregate.Event { return OAuthRefreshTokenRevoked{r.Snapshot()} }},
		{"revoke twice", func(t *testing.T, r *OAuthRefreshToken) { r.Revoke(); r.PullEvents(); r.Revoke() }, nil},
	})
}

func TestOrganizationDomainEvents(t *testing.T) {
	clock.Fix(t, t0)
	checkEvents(t, orgDomainNew, []eventCase[*OrganizationDomain]{
		{"verify", func(t *testing.T, d *OrganizationDomain) { d.Verify() },
			func(d *OrganizationDomain) aggregate.Event { return OrganizationDomainVerified{d.Snapshot()} }},
		{"verify twice", func(t *testing.T, d *OrganizationDomain) { d.Verify(); d.PullEvents(); d.Verify() }, nil},
		{"rechallenge", func(t *testing.T, d *OrganizationDomain) {
			d.Verify()
			d.PullEvents()
			require.NoError(t, d.Rechallenge(testHash("txt-token")))
		}, func(d *OrganizationDomain) aggregate.Event { return OrganizationDomainRechallenged{d.Snapshot()} }},
		{"rechallenge with the same token", func(t *testing.T, d *OrganizationDomain) {
			require.NoError(t, d.Rechallenge(testHash("txt-token")))
		}, nil},
	})
}

func TestPasswordCredentialEvents(t *testing.T) {
	clock.Fix(t, t0)
	checkEvents(t, passwordNew, []eventCase[*PasswordCredential]{
		{"change", func(t *testing.T, c *PasswordCredential) { require.NoError(t, c.Change(pwOtherHash)) },
			func(c *PasswordCredential) aggregate.Event { return PasswordCredentialChanged{c.Snapshot()} }},
		{"change to the same hash", func(t *testing.T, c *PasswordCredential) { require.NoError(t, c.Change(pwHash)) }, nil},
	})
}

func TestRoleEvents(t *testing.T) {
	clock.Fix(t, t0)
	checkEvents(t, roleCustom, []eventCase[*Role]{
		{"rename", func(t *testing.T, r *Role) { require.NoError(t, r.Rename("Release lead")) },
			func(r *Role) aggregate.Event { return RoleRenamed{r.Snapshot()} }},
		{"rename to the same name", func(t *testing.T, r *Role) { require.NoError(t, r.Rename("Release manager")) }, nil},
	})
}

func TestSigningKeyEvents(t *testing.T) {
	clock.Fix(t, t0)
	checkEvents(t, keyNew, []eventCase[*SigningKey]{
		{"start retiring", func(t *testing.T, k *SigningKey) { require.NoError(t, k.StartRetiring(t2)) },
			func(k *SigningKey) aggregate.Event {
				return SigningKeyStatusChanged{SigningKey: k.Snapshot(), PreviousStatus: SigningKeyActive}
			}},
		{"start retiring twice", func(t *testing.T, k *SigningKey) {
			require.NoError(t, k.StartRetiring(t2))
			k.PullEvents()
			require.NoError(t, k.StartRetiring(t2))
		}, nil},
		{"retire a retiring key", func(t *testing.T, k *SigningKey) {
			require.NoError(t, k.StartRetiring(t2))
			k.PullEvents()
			k.Retire()
		}, func(k *SigningKey) aggregate.Event {
			return SigningKeyStatusChanged{SigningKey: k.Snapshot(), PreviousStatus: SigningKeyRetiring}
		}},
		{"retire twice", func(t *testing.T, k *SigningKey) { k.Retire(); k.PullEvents(); k.Retire() }, nil},
	})
}

func TestWorkloadCredentialEvents(t *testing.T) {
	clock.Fix(t, t0)
	checkEvents(t, func(t *testing.T) *WorkloadCredential { return wcrNew(t, wcrProps()) }, []eventCase[*WorkloadCredential]{
		{"revoke", func(t *testing.T, c *WorkloadCredential) { c.Revoke() },
			func(c *WorkloadCredential) aggregate.Event { return WorkloadCredentialRevoked{c.Snapshot()} }},
		{"revoke twice", func(t *testing.T, c *WorkloadCredential) { c.Revoke(); c.PullEvents(); c.Revoke() }, nil},
	})
}

func TestWorkloadEnrollmentTokenEvents(t *testing.T) {
	clock.Fix(t, t0)
	checkEvents(t, wetNew, []eventCase[*WorkloadEnrollmentToken]{
		{"redeem", func(t *testing.T, tok *WorkloadEnrollmentToken) {
			require.NoError(t, tok.Redeem(wetWorkload(t, testOrg, "eu-west")))
		}, func(tok *WorkloadEnrollmentToken) aggregate.Event {
			return WorkloadEnrollmentTokenRedeemed{tok.Snapshot()}
		}},
		{"redeem for another pool", func(t *testing.T, tok *WorkloadEnrollmentToken) {
			require.Error(t, tok.Redeem(wetWorkload(t, testOrg, "us-east")))
		}, nil},
	})
}

// No credential material may reach an event: every hash is left out of the
// snapshots' JSON.
func TestEventsCarryNoCredentialMaterial(t *testing.T) {
	clock.Fix(t, t0)
	code := codeNew(t)
	require.NoError(t, code.Redeem(testClient, codeRedirect, codeVerifier))
	tests := []struct {
		name   string
		events []aggregate.Event
		secret string
	}{
		{"api token", tokenNew(t, tokenProps()).PullEvents(), base64.StdEncoding.EncodeToString(testHash("ivt_a1b2c3d4"))},
		{"oauth refresh token", refreshNew(t).PullEvents(), base64.StdEncoding.EncodeToString(testHash("refresh-1"))},
		{"oauth authorization code", code.PullEvents(), base64.StdEncoding.EncodeToString(testHash("code"))},
		{"workload enrollment token", wetNew(t).PullEvents(), base64.StdEncoding.EncodeToString(testHash("wet-secret"))},
		{"organization domain", orgDomainNew(t).PullEvents(), base64.StdEncoding.EncodeToString(testHash("txt-token"))},
		{"password credential", passwordNew(t).PullEvents(), pwHash},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Len(t, tt.events, 1)
			b, err := json.Marshal(tt.events[0])
			require.NoError(t, err)

			assert.NotContains(t, string(b), tt.secret)
			assert.NotContains(t, string(b), "hash")
			assert.NotContains(t, string(b), "Hash")
		})
	}
}

// The snapshots' JSON keys are the events' contract: a change here is a
// change for every consumer.
func TestSnapshotJSONKeys(t *testing.T) {
	principal := []string{"id", "organization_id", "kind", "display_name", "status", "metadata", "created_at", "updated_at"}
	tests := []struct {
		snapshot any
		keys     []string
	}{
		{OrganizationSnapshot{}, []string{"id", "slug", "name", "display_name", "logo_url", "status", "metadata", "created_at", "updated_at"}},
		{AccountSnapshot{}, []string{"id", "email", "email_verified_at", "display_name", "status", "created_at", "updated_at"}},
		{PrincipalSnapshot{}, principal},
		{UserSnapshot{}, append(slices.Clone(principal), "account_id", "joined_at", "last_signed_in_at")},
		{ServiceAccountSnapshot{}, append(slices.Clone(principal), "description", "created_by_principal_id")},
		{AgentSnapshot{}, append(slices.Clone(principal), "slug")},
		{AgentSessionSnapshot{}, append(slices.Clone(principal), "session_id", "work_item_id", "delegator_principal_id", "expires_at", "revoked_at", "scope_resource_ids")},
		{WorkloadSnapshot{}, append(slices.Clone(principal), "component", "pool", "enrolled_at", "last_seen_at")},
		{APITokenSnapshot{}, []string{"id", "organization_id", "principal_id", "name", "token_prefix", "expires_at", "last_used_at", "revoked_at", "created_at"}},
		{ExternalIdentitySnapshot{}, []string{"id", "organization_id", "principal_id", "identity_provider_id", "subject", "last_login_at", "created_at"}},
		{AccountIdentitySnapshot{}, []string{"id", "account_id", "provider", "subject", "last_login_at", "created_at"}},
		{GrantSnapshot{}, []string{"id", "organization_id", "principal_id", "group_id", "role_id", "resource_id", "granted_by_principal_id", "expires_at", "created_at"}},
		{GroupSnapshot{}, []string{"id", "organization_id", "name", "source", "external_id", "created_at", "updated_at"}},
		{GroupMemberSnapshot{}, []string{"organization_id", "group_id", "principal_id", "created_at"}},
		{IdentityProviderSnapshot{}, []string{"id", "organization_id", "protocol", "name", "config", "managed_by_configuration", "secret_ref", "jit_provisioning", "status", "created_at", "updated_at"}},
		{IdentityProviderConfig{}, []string{"issuer", "client_id", "metadata_url", "base_url", "allowed_domains", "allowed_organizations", "allowed_emails"}},
		{OAuthClientSnapshot{}, []string{"id", "client_id", "registration", "name", "redirect_uris", "metadata_url", "created_at", "last_used_at"}},
		{OAuthAuthorizationCodeSnapshot{}, []string{"organization_id", "principal_id", "client_id", "redirect_uri", "code_challenge", "resource", "scopes", "expires_at", "used_at"}},
		{OAuthRefreshTokenSnapshot{}, []string{"id", "organization_id", "principal_id", "client_id", "family_id", "resource", "scopes", "expires_at", "replaced_by", "revoked_at", "created_at"}},
		{OrganizationDomainSnapshot{}, []string{"organization_id", "domain", "verified_at", "created_at"}},
		{PasswordCredentialSnapshot{}, []string{"account_id", "updated_at"}},
		{RoleSnapshot{}, []string{"id", "organization_id", "key", "name", "created_at"}},
		{SigningKeySnapshot{}, []string{"kid", "organization_id", "algorithm", "public_jwk", "private_key_ref", "status", "created_at", "not_after"}},
		{WorkloadCredentialSnapshot{}, []string{"id", "organization_id", "principal_id", "kind", "identifier", "not_after", "revoked_at", "replaced_by", "created_at"}},
		{WorkloadEnrollmentTokenSnapshot{}, []string{"id", "organization_id", "pool", "target_id", "credential_kind", "created_by_principal_id", "expires_at", "used_at", "used_by_principal_id", "created_at"}},
	}
	for _, tt := range tests {
		b, err := json.Marshal(tt.snapshot)
		require.NoError(t, err)
		var fields map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(b, &fields))

		assert.ElementsMatch(t, tt.keys, slices.Collect(maps.Keys(fields)), "%T", tt.snapshot)
	}
}
