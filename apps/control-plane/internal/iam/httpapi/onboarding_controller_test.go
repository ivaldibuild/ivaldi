package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/api"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/config"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam/onboarding"
	"github.com/ivaldibuild/ivaldi/pkg/logging"
)

var _ api.Routes = (*OnboardingController)(nil)

type fakeOnboarding struct {
	got *iam.OnboardOrganizationInput
	err error
}

func (f *fakeOnboarding) OnboardOrganization(_ context.Context, in *iam.OnboardOrganizationInput) error {
	f.got = in
	return f.err
}

const onboardingBody = `{
	"display_name": "Ada Lovelace",
	"email": "ada@example.com",
	"password": "correct-horse",
	"organization_name": "Analytical Engines Ltd",
	"organization_display_name": "Analytical Engines"
}`

func postOnboarding(t *testing.T, svc iam.Onboarding, body string) *httptest.ResponseRecorder {
	t.Helper()
	port := 0
	s, err := api.New(&config.Config{Port: &port}, "test", logging.NewMockLogger(), NewOnboardingController(svc))
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, api.BasePath+"/onboarding", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

func TestOnboardWithEmail(t *testing.T) {
	t.Run("created", func(t *testing.T) {
		svc := &fakeOnboarding{}

		rec := postOnboarding(t, svc, onboardingBody)

		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
		require.NotNil(t, svc.got)
		assert.Equal(t, "ada@example.com", svc.got.OwnerEmail)
		assert.Equal(t, "Ada Lovelace", svc.got.OwnerDisplayName)
		assert.Equal(t, "Analytical Engines Ltd", svc.got.OrganizationName)
		assert.Equal(t, "Analytical Engines", svc.got.OrganizationDisplayName)
		assert.Nil(t, svc.got.OrganizationSlug)
	})

	t.Run("invalid body never reaches the service", func(t *testing.T) {
		svc := &fakeOnboarding{}

		rec := postOnboarding(t, svc, `{"email": "not-an-email"}`)

		assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
		assert.Nil(t, svc.got)
	})

	t.Run("account conflict", func(t *testing.T) {
		rec := postOnboarding(t, &fakeOnboarding{err: onboarding.NewErrAccountConflict("taken")}, onboardingBody)

		assert.Equal(t, http.StatusConflict, rec.Code)
	})

	t.Run("organization conflict", func(t *testing.T) {
		rec := postOnboarding(t, &fakeOnboarding{err: onboarding.NewErrOrganizationConflict("taken")}, onboardingBody)

		assert.Equal(t, http.StatusConflict, rec.Code)
	})

	t.Run("invalid input the domain refuses", func(t *testing.T) {
		rec := postOnboarding(t, &fakeOnboarding{err: iam.NewErrInvalidInput("the logo URL must use https")}, onboardingBody)

		assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
		assert.Contains(t, rec.Body.String(), "the logo URL must use https")
	})

	t.Run("other errors", func(t *testing.T) {
		rec := postOnboarding(t, &fakeOnboarding{err: errors.New("database down")}, onboardingBody)

		assert.Equal(t, http.StatusInternalServerError, rec.Code)
		assert.NotContains(t, rec.Body.String(), "database down", "internal errors are not shown to the caller")
	})
}
