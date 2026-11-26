package brokers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/aws/smithy-go"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/credentials"
)

// awsBroker assumes the store's role with the control plane's own AWS
// identity, with the store's ID as the external ID against the confused
// deputy, for 15 minutes, the shortest AWS allows, under a session policy
// that allows reading only the Deployment's secrets. AWS sessions cannot be
// revoked one by one; they expire.
type awsBroker struct {
	roles roleAssumer
}

// assumeRole is one AssumeRole call.
type assumeRole struct {
	Region, RoleARN, SessionName, ExternalID, Policy string
	Duration                                         time.Duration
}

// roleAssumer assumes a role; awsRoles uses the default credential chain.
type roleAssumer interface {
	AssumeRole(ctx context.Context, in assumeRole) (credentials.BrokeredCredential, time.Time, error)
}

func (a *awsBroker) Mint(ctx context.Context, r credentials.MintRequest) (credentials.Minted, error) {
	settings := r.Store.Settings().AWS
	if settings == nil {
		return credentials.Minted{}, credentials.NewErrInvalidInput("an aws_secrets_manager store names its region")
	}
	roleARN := r.Store.Access().RoleARN
	policy, err := sessionPolicy(settings.Region, roleARN, r.Paths)
	if err != nil {
		return credentials.Minted{}, err
	}
	session := "ivaldi-" + r.DeploymentID
	creds, expires, err := a.roles.AssumeRole(ctx, assumeRole{
		Region: settings.Region, RoleARN: roleARN, SessionName: session, ExternalID: r.Store.ID(),
		Policy: policy, Duration: credentials.BrokeredLifetime,
	})
	if err != nil {
		return credentials.Minted{}, fmt.Errorf("assume %s: %w", roleARN, err)
	}
	return credentials.Minted{Credential: creds, ExpiresAt: expires, ProviderID: session}, nil
}

func (a *awsBroker) Revoke(context.Context, credentials.RevokeRequest) (bool, error) {
	return false, nil
}

// sessionPolicy allows reading exactly the secrets at paths: each an ARN, or
// a name, which AWS suffixes with six random characters. Decrypting is
// allowed only through Secrets Manager, for secrets under a customer key.
func sessionPolicy(region, roleARN string, paths []string) (string, error) {
	// arn:<partition>:iam::<account>:role/<name>
	parts := strings.SplitN(roleARN, ":", 6)
	if len(parts) != 6 || parts[0] != "arn" || parts[2] != "iam" || parts[4] == "" || !strings.HasPrefix(parts[5], "role/") {
		return "", credentials.NewErrInvalidInput(fmt.Sprintf("%q is not an IAM role ARN", roleARN))
	}
	partition, account := parts[1], parts[4]
	var resources []string
	for _, p := range paths {
		resource := p
		if !strings.HasPrefix(p, "arn:") {
			resource = fmt.Sprintf("arn:%s:secretsmanager:%s:%s:secret:%s-??????", partition, region, account, p)
		}
		if !slices.Contains(resources, resource) {
			resources = append(resources, resource)
		}
	}
	if len(resources) == 0 {
		return "", credentials.NewErrInvalidInput("a brokered aws_secrets_manager store is granted for the secrets it holds; none were named")
	}
	policy, err := json.Marshal(map[string]any{
		"Version": "2012-10-17",
		"Statement": []map[string]any{
			{"Effect": "Allow", "Action": "secretsmanager:GetSecretValue", "Resource": resources},
			{"Effect": "Allow", "Action": "secretsmanager:BatchGetSecretValue", "Resource": "*"},
			{"Effect": "Allow", "Action": "kms:Decrypt", "Resource": "*",
				"Condition": map[string]any{"StringEquals": map[string]string{"kms:ViaService": "secretsmanager." + region + ".amazonaws.com"}}},
		},
	})
	return string(policy), err
}

// awsRoles assumes roles with the control plane's own AWS identity, from
// the default credential chain.
type awsRoles struct{}

func (awsRoles) AssumeRole(ctx context.Context, in assumeRole) (credentials.BrokeredCredential, time.Time, error) {
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(in.Region))
	if err != nil {
		return credentials.BrokeredCredential{}, time.Time{}, fmt.Errorf("%w: aws configuration: %v", credentials.ErrStoreUnavailable, err)
	}
	out, err := sts.NewFromConfig(cfg).AssumeRole(ctx, &sts.AssumeRoleInput{
		RoleArn: aws.String(in.RoleARN), RoleSessionName: aws.String(in.SessionName), ExternalId: aws.String(in.ExternalID),
		Policy: aws.String(in.Policy), DurationSeconds: aws.Int32(int32(in.Duration.Seconds())),
	})
	if err != nil {
		return credentials.BrokeredCredential{}, time.Time{}, awsError(err)
	}
	c := out.Credentials
	return credentials.BrokeredCredential{
		AccessKeyID: aws.ToString(c.AccessKeyId), SecretAccessKey: aws.ToString(c.SecretAccessKey), SessionToken: aws.ToString(c.SessionToken),
	}, aws.ToTime(c.Expiration), nil
}

// awsError maps STS's answer: a refusal, such as a role that does not trust
// the control plane or a policy too large, or anything else, worth a retry.
func awsError(err error) error {
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.ErrorCode() {
		case "Throttling", "ThrottlingException", "RequestLimitExceeded", "ServiceUnavailable", "InternalFailure":
			return fmt.Errorf("%w: %s", credentials.ErrStoreUnavailable, apiErr.ErrorCode())
		}
		return fmt.Errorf("%w: %s: %s", credentials.ErrStoreRefused, apiErr.ErrorCode(), apiErr.ErrorMessage())
	}
	return fmt.Errorf("%w: %v", credentials.ErrStoreUnavailable, err)
}
