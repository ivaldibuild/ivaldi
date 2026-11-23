package secretstore

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

// The exchanges a reader makes for a store in federated mode: it trades a
// JWT the control plane signed, which the store's configuration trusts, for
// the store's own short-lived credential. Vault's is its JWT login
// (VaultConfig with the JWT auth method's mount).

// DopplerOIDC exchanges a JWT for a Doppler service account token, at the
// service account identity that trusts its issuer. api is Doppler's API, or
// empty for DefaultDopplerAPI; client may be nil.
func DopplerOIDC(ctx context.Context, client *http.Client, api, identityID, jwt string) (string, error) {
	if api == "" {
		api = DefaultDopplerAPI
	}
	if client == nil {
		client = http.DefaultClient
	}
	body, err := json.Marshal(map[string]string{"identity": identityID, "token": jwt})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(api, "/")+"/v3/auth/oidc", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: doppler oidc login: %v", ErrUnavailable, err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
		return "", fmt.Errorf("%w: doppler oidc login answered %d", ErrUnavailable, resp.StatusCode)
	case resp.StatusCode != http.StatusOK:
		// The identity refused the JWT: its issuer, audience, or subject
		// do not match the identity's rules.
		return "", fmt.Errorf("%w: doppler oidc login with identity %s answered %d", ErrDenied, identityID, resp.StatusCode)
	}
	var out struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil || out.Token == "" {
		return "", fmt.Errorf("%w: doppler oidc login gave no token", ErrUnavailable)
	}
	return out.Token, nil
}

// NewAWSWebIdentityClient returns a Secrets Manager client in region with
// credentials for roleARN, assumed with AssumeRoleWithWebIdentity using jwt.
// The session is named sessionName, which CloudTrail records. The call
// needs no AWS credentials of the reader's own.
func NewAWSWebIdentityClient(ctx context.Context, region, roleARN, sessionName, jwt string) (SecretsManagerAPI, error) {
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("aws configuration: %w", err)
	}
	cfg.Credentials = aws.NewCredentialsCache(stscreds.NewWebIdentityRoleProvider(
		sts.NewFromConfig(cfg), roleARN, staticToken(jwt),
		func(o *stscreds.WebIdentityRoleOptions) { o.RoleSessionName = sessionName },
	))
	return secretsmanager.NewFromConfig(cfg), nil
}

// staticToken is a web identity token known in advance.
type staticToken string

func (t staticToken) GetIdentityToken() ([]byte, error) { return []byte(t), nil }
