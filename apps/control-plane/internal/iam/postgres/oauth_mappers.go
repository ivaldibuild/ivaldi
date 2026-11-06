package postgres

import (
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi/pkg/database"
)

type oauthClientMapper struct{}

func newOAuthClientMapper() *oauthClientMapper {
	return &oauthClientMapper{}
}

func (m *oauthClientMapper) ToPersistence(c *iam.OAuthClient) *oauthClientEntity {
	s := c.Snapshot()
	return &oauthClientEntity{
		ID:           s.ID,
		ClientID:     s.ClientID,
		Registration: string(s.Registration),
		Name:         s.Name,
		RedirectURIs: database.TextArray(s.RedirectURIs),
		MetadataURL:  s.MetadataURL,
		CreatedAt:    s.CreatedAt,
		LastUsedAt:   s.LastUsedAt,
	}
}

func (m *oauthClientMapper) ToDomain(e *oauthClientEntity) (*iam.OAuthClient, error) {
	return iam.NewOAuthClient(iam.NewOAuthClientProps{
		ClientID:     e.ClientID,
		Registration: iam.ClientRegistration(e.Registration),
		Name:         e.Name,
		RedirectURIs: []string(e.RedirectURIs),
		MetadataURL:  e.MetadataURL,
		CreatedAt:    utc(e.CreatedAt),
		LastUsedAt:   utcPtr(e.LastUsedAt),
	}, &e.ID)
}

type authorizationCodeMapper struct{}

func newAuthorizationCodeMapper() *authorizationCodeMapper {
	return &authorizationCodeMapper{}
}

func (m *authorizationCodeMapper) ToPersistence(c *iam.OAuthAuthorizationCode) *authorizationCodeEntity {
	s := c.Snapshot()
	return &authorizationCodeEntity{
		CodeHash:       s.CodeHash,
		OrganizationID: s.OrganizationID,
		PrincipalID:    s.PrincipalID,
		ClientID:       s.ClientID,
		RedirectURI:    s.RedirectURI,
		CodeChallenge:  s.CodeChallenge,
		Resource:       s.Resource,
		Scopes:         database.TextArray(s.Scopes),
		ExpiresAt:      s.ExpiresAt,
		UsedAt:         s.UsedAt,
	}
}

func (m *authorizationCodeMapper) ToDomain(e *authorizationCodeEntity) (*iam.OAuthAuthorizationCode, error) {
	return iam.NewOAuthAuthorizationCode(iam.NewOAuthAuthorizationCodeProps{
		CodeHash:       e.CodeHash,
		OrganizationID: e.OrganizationID,
		PrincipalID:    e.PrincipalID,
		ClientID:       e.ClientID,
		RedirectURI:    e.RedirectURI,
		CodeChallenge:  e.CodeChallenge,
		Resource:       e.Resource,
		Scopes:         []string(e.Scopes),
		ExpiresAt:      *utc(e.ExpiresAt),
		UsedAt:         utcPtr(e.UsedAt),
	})
}

type refreshTokenMapper struct{}

func newRefreshTokenMapper() *refreshTokenMapper {
	return &refreshTokenMapper{}
}

func (m *refreshTokenMapper) ToPersistence(t *iam.OAuthRefreshToken) *refreshTokenEntity {
	s := t.Snapshot()
	return &refreshTokenEntity{
		ID:             s.ID,
		OrganizationID: s.OrganizationID,
		PrincipalID:    s.PrincipalID,
		ClientID:       s.ClientID,
		FamilyID:       s.FamilyID,
		TokenHash:      s.TokenHash,
		Resource:       s.Resource,
		Scopes:         database.TextArray(s.Scopes),
		ExpiresAt:      s.ExpiresAt,
		ReplacedBy:     s.ReplacedBy,
		RevokedAt:      s.RevokedAt,
		CreatedAt:      s.CreatedAt,
	}
}

func (m *refreshTokenMapper) ToDomain(e *refreshTokenEntity) (*iam.OAuthRefreshToken, error) {
	return iam.NewOAuthRefreshToken(iam.NewOAuthRefreshTokenProps{
		OrganizationID: e.OrganizationID,
		PrincipalID:    e.PrincipalID,
		ClientID:       e.ClientID,
		FamilyID:       e.FamilyID,
		TokenHash:      e.TokenHash,
		Resource:       e.Resource,
		Scopes:         []string(e.Scopes),
		ExpiresAt:      *utc(e.ExpiresAt),
		ReplacedBy:     e.ReplacedBy,
		RevokedAt:      utcPtr(e.RevokedAt),
		CreatedAt:      utc(e.CreatedAt),
	}, &e.ID)
}
