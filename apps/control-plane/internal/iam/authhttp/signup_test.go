package authhttp

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam/connector/connectortest"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam/onboarding"
)

type fakeOnboarding struct {
	got []iam.OnboardWithPlatformIdentityInput
	err error
}

func (f *fakeOnboarding) OnboardOrganization(context.Context, *iam.OnboardOrganizationInput) error {
	return errors.New("unused")
}

func (f *fakeOnboarding) ProvisionInstallation(context.Context, iam.ProvisionInstallationInput) (*iam.Organization, error) {
	return nil, errors.New("unused")
}

func (f *fakeOnboarding) OnboardWithPlatformIdentity(_ context.Context, in iam.OnboardWithPlatformIdentityInput) (string, error) {
	f.got = append(f.got, in)
	if f.err != nil {
		return "", f.err
	}
	return "acc_new", nil
}

func newCloudRig(t *testing.T, subject string, ob *fakeOnboarding) *rig {
	t.Helper()
	gh := &connectortest.Fake{Kind: iam.ProtocolGitHub, Name: "GitHub", Profile: iam.ExternalProfile{Subject: subject, Email: "grace@example.com", EmailVerified: true, DisplayName: "Grace"}}
	opts := []Option{WithProviders(Provider{Kind: "github", Connector: gh, Platform: iam.PlatformGitHub})}
	if ob != nil {
		opts = append(opts, WithOnboarding(ob))
	}
	return newRig(t, opts...)
}

func TestPlatformSignUp(t *testing.T) {
	ob := &fakeOnboarding{}
	r := newCloudRig(t, "new", ob)
	r.auth.external["new"] = iam.ErrSignUpRequired
	back := "/oauth/authorize?client_id=ivaldictl"

	assert.Contains(t, r.get(LoginPath).Body.String(), "creates your account")

	resp := startAndReturn(t, r, back)
	require.Equal(t, http.StatusSeeOther, resp.StatusCode)
	assert.Equal(t, SignUpPath, resp.Header.Get("Location"))
	assert.NotContains(t, r.jar, "__Host-"+signInCookie, "not signed in until the sign-up finishes")

	page := r.get(SignUpPath)
	require.Equal(t, http.StatusOK, page.Code)
	assert.Contains(t, page.Body.String(), "grace@example.com")
	csrf := csrfField.FindStringSubmatch(page.Body.String())[1]

	rec := r.post(SignUpPath, url.Values{"csrf": {csrf}, "name": {"Compilers Inc"}, "slug": {"compilers"}}, testIssuer)

	require.Equal(t, http.StatusSeeOther, rec.Code)
	assert.Equal(t, back, rec.Header().Get("Location"), "on to the authorization request the sign-up interrupted")
	require.Len(t, ob.got, 1)
	assert.Equal(t, iam.OnboardWithPlatformIdentityInput{
		Provider:         iam.PlatformGitHub,
		Profile:          iam.ExternalProfile{Subject: "new", Email: "grace@example.com", EmailVerified: true, DisplayName: "Grace"},
		OrganizationName: "Compilers Inc", OrganizationSlug: new("compilers"),
	}, ob.got[0])
	var got signIn
	require.NoError(t, r.codec.Decode(signInCookie, r.jar["__Host-"+signInCookie].Value, &got))
	assert.Equal(t, "acc_new", got.AccountID)
	assert.NotContains(t, r.jar, "__Host-"+signUpCookie)
}

func TestPlatformSignUpShowsConflicts(t *testing.T) {
	ob := &fakeOnboarding{err: onboarding.NewErrOrganizationConflict("taken")}
	r := newCloudRig(t, "new", ob)
	r.auth.external["new"] = iam.ErrSignUpRequired
	startAndReturn(t, r, "")
	csrf := csrfField.FindStringSubmatch(r.get(SignUpPath).Body.String())[1]

	rec := r.post(SignUpPath, url.Values{"csrf": {csrf}, "name": {"Taken"}}, testIssuer)

	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Contains(t, rec.Body.String(), `value="Taken"`, "the form keeps what was typed")
	assert.Contains(t, r.jar, "__Host-"+signUpCookie, "the person can try another name")
}

func TestPlatformSignInOfAnExistingAccount(t *testing.T) {
	r := newCloudRig(t, "known", &fakeOnboarding{})

	resp := startAndReturn(t, r, "/oauth/authorize?client_id=ivaldictl")

	require.Equal(t, http.StatusSeeOther, resp.StatusCode)
	var got signIn
	require.NoError(t, r.codec.Decode(signInCookie, r.jar["__Host-"+signInCookie].Value, &got))
	assert.Equal(t, signIn{AccountID: "acc_known", Method: iam.SignInPlatform}, got, "not restricted to one Organization")
}

func TestSignUpRefuses(t *testing.T) {
	t.Run("without a pending sign-up", func(t *testing.T) {
		r := newCloudRig(t, "new", &fakeOnboarding{})
		assert.Equal(t, http.StatusBadRequest, r.get(SignUpPath).Code)
	})
	t.Run("when sign-up is off", func(t *testing.T) {
		r := newCloudRig(t, "new", nil)
		r.auth.external["new"] = iam.ErrSignUpRequired
		assert.Equal(t, http.StatusForbidden, startAndReturn(t, r, "").StatusCode)
		assert.NotContains(t, r.get(LoginPath).Body.String(), "creates your account")
	})
}
