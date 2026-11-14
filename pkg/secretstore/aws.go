package secretstore

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

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

// stagePrefix marks a Ref version that names a staging label rather than a
// VersionId, as in "stage:AWSPREVIOUS".
const stagePrefix = "stage:"

// awsStages are the staging labels Secrets Manager itself uses; they are read
// as stages without the prefix.
var awsStages = []string{"AWSCURRENT", "AWSPREVIOUS", "AWSPENDING"}

// versionParams reads a Ref version: a staging label when it is one of AWS's
// or carries the stage: prefix, and otherwise a VersionId, which may be a
// UUID or any 32-64 character client request token, and is what a resolved
// value records and a pin replays.
func versionParams(version string) (id, stage *string) {
	switch {
	case version == "":
		return nil, nil
	case strings.HasPrefix(version, stagePrefix):
		return nil, aws.String(strings.TrimPrefix(version, stagePrefix))
	case slices.Contains(awsStages, version):
		return nil, aws.String(version)
	default:
		return aws.String(version), nil
	}
}

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
// VersionId, or a staging label: AWSCURRENT, AWSPREVIOUS, AWSPENDING, or any
// other written as stage:<label>.
func NewAWS(api SecretsManagerAPI) Store { return &awsStore{api: api} }

type awsStore struct{ api SecretsManagerAPI }

func (a *awsStore) Kind() Kind     { return KindAWSSecretsManager }
func (a *awsStore) Versions() bool { return true }

func (a *awsStore) Get(ctx context.Context, ref Ref) (Value, error) {
	in := &secretsmanager.GetSecretValueInput{SecretId: aws.String(ref.Path)}
	in.VersionId, in.VersionStage = versionParams(ref.Version)
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
		// A batch answers 200 and lists each secret it could not read in
		// Errors, so those are mapped like a single read's error.
		failed := map[string]error{}
		for _, e := range resp.Errors {
			failed[aws.ToString(e.SecretId)] = codeError(aws.ToString(e.ErrorCode), false, false)
		}
		for _, i := range chunk {
			if err, ok := failed[refs[i].Path]; ok {
				return nil, fail(KindAWSSecretsManager, refs[i], err)
			}
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
	return codeError(api.ErrorCode(), pinned, api.ErrorFault() == smithy.FaultServer)
}

// codeError maps a Secrets Manager error code, from an API error or a batch's
// per-secret Errors, onto the store's errors.
func codeError(code string, pinned, serverFault bool) error {
	switch code {
	case "ResourceNotFoundException":
		if pinned {
			return ErrVersionGone
		}
		return ErrNotFound
	case "AccessDeniedException", "UnrecognizedClientException", "InvalidSignatureException", "ExpiredTokenException",
		"DecryptionFailure":
		return fmt.Errorf("%w: %s", ErrDenied, code)
	case "ThrottlingException", "InternalServiceError", "ServiceUnavailable", "RequestLimitExceeded":
		return fmt.Errorf("%w: %s", ErrUnavailable, code)
	}
	if serverFault {
		return fmt.Errorf("%w: %s", ErrUnavailable, code)
	}
	return fmt.Errorf("%w: %s", ErrDenied, code)
}
