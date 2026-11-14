package secretstore

import (
	"context"
	"errors"
	"fmt"
	"regexp"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/aws/smithy-go"
)

// SecretsManagerAPI is the part of the Secrets Manager client the store
// uses.
type SecretsManagerAPI interface {
	GetSecretValue(ctx context.Context, in *secretsmanager.GetSecretValueInput, opts ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error)
	BatchGetSecretValue(ctx context.Context, in *secretsmanager.BatchGetSecretValueInput, opts ...func(*secretsmanager.Options)) (*secretsmanager.BatchGetSecretValueOutput, error)
}

// awsBatchLimit is the most secrets one BatchGetSecretValue call names.
const awsBatchLimit = 20

// versionID matches a Secrets Manager VersionId; anything else is a stage.
var versionID = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// NewAWSClient returns a Secrets Manager client in region with the Pod's
// credentials (EKS Pod Identity or IRSA), assuming roleARN on top when it is
// set.
func NewAWSClient(ctx context.Context, region, roleARN string) (SecretsManagerAPI, error) {
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("aws configuration: %w", err)
	}
	if roleARN != "" {
		cfg.Credentials = aws.NewCredentialsCache(stscreds.NewAssumeRoleProvider(sts.NewFromConfig(cfg), roleARN))
	}
	return secretsmanager.NewFromConfig(cfg), nil
}

// NewAWS returns the aws_secrets_manager store over api. A Ref's Path is the
// secret's name or ARN, its Field a key of a JSON secret, and its Version a
// VersionId or a staging label such as AWSPREVIOUS.
func NewAWS(api SecretsManagerAPI) Store { return &awsStore{api: api} }

type awsStore struct{ api SecretsManagerAPI }

func (a *awsStore) Kind() Kind     { return KindAWSSecretsManager }
func (a *awsStore) Versions() bool { return true }

func (a *awsStore) Get(ctx context.Context, ref Ref) (Value, error) {
	in := &secretsmanager.GetSecretValueInput{SecretId: aws.String(ref.Path)}
	switch {
	case versionID.MatchString(ref.Version):
		in.VersionId = aws.String(ref.Version)
	case ref.Version != "":
		in.VersionStage = aws.String(ref.Version)
	}
	out, err := a.api.GetSecretValue(ctx, in)
	if err != nil {
		return Value{}, fail(KindAWSSecretsManager, ref, awsError(err, ref.Version != ""))
	}
	return a.value(ref, out.SecretString, out.VersionId)
}

// GetMany batches the refs that name no version, up to awsBatchLimit per
// call, and reads pinned ones one by one.
func (a *awsStore) GetMany(ctx context.Context, refs []Ref) ([]Value, error) {
	out := make([]Value, len(refs))
	var latest []int
	for i, ref := range refs {
		if ref.Version != "" {
			v, err := a.Get(ctx, ref)
			if err != nil {
				return nil, err
			}
			out[i] = v
			continue
		}
		latest = append(latest, i)
	}
	for start := 0; start < len(latest); start += awsBatchLimit {
		chunk := latest[start:min(start+awsBatchLimit, len(latest))]
		ids := make([]string, 0, len(chunk))
		for _, i := range chunk {
			ids = append(ids, refs[i].Path)
		}
		resp, err := a.api.BatchGetSecretValue(ctx, &secretsmanager.BatchGetSecretValueInput{SecretIdList: ids})
		if err != nil {
			return nil, fail(KindAWSSecretsManager, refs[chunk[0]], awsError(err, false))
		}
		found := map[string]int{}
		for j, v := range resp.SecretValues {
			found[aws.ToString(v.Name)] = j
			found[aws.ToString(v.ARN)] = j
		}
		for _, i := range chunk {
			j, ok := found[refs[i].Path]
			if !ok {
				return nil, fail(KindAWSSecretsManager, refs[i], ErrNotFound)
			}
			v, err := a.value(refs[i], resp.SecretValues[j].SecretString, resp.SecretValues[j].VersionId)
			if err != nil {
				return nil, err
			}
			out[i] = v
		}
	}
	return out, nil
}

func (a *awsStore) value(ref Ref, secret, version *string) (Value, error) {
	if secret == nil {
		return Value{}, fail(KindAWSSecretsManager, ref, fmt.Errorf("%w: the secret is binary, not a string", ErrFieldMissing))
	}
	v, err := field(*secret, ref.Field)
	if err != nil {
		return Value{}, fail(KindAWSSecretsManager, ref, err)
	}
	return Value{Value: v, Version: aws.ToString(version), Source: ref}, nil
}

// awsError maps a Secrets Manager error onto the store's errors.
func awsError(err error, pinned bool) error {
	var api smithy.APIError
	if !errors.As(err, &api) {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	switch api.ErrorCode() {
	case "ResourceNotFoundException":
		if pinned {
			return ErrVersionGone
		}
		return ErrNotFound
	case "AccessDeniedException", "UnrecognizedClientException", "InvalidSignatureException", "ExpiredTokenException":
		return fmt.Errorf("%w: %s", ErrDenied, api.ErrorCode())
	case "DecryptionFailure":
		return fmt.Errorf("%w: %s", ErrDenied, api.ErrorCode())
	case "ThrottlingException", "InternalServiceError", "ServiceUnavailable":
		return fmt.Errorf("%w: %s", ErrUnavailable, api.ErrorCode())
	}
	if api.ErrorFault() == smithy.FaultServer {
		return fmt.Errorf("%w: %s", ErrUnavailable, api.ErrorCode())
	}
	return fmt.Errorf("%w: %s", ErrDenied, api.ErrorCode())
}
