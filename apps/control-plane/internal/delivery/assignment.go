package delivery

// DeployerKind names how an agent applies a Release. Helm is the only
// deployer so far; Argo CD and Flux add their own value and sub-object
// without changing the rest of the assignment.
type DeployerKind string

const (
	DeployerHelm DeployerKind = "helm"
)

// ApplyAssignment is the deployer-neutral part of an assignment: what to
// apply, where, and with which values. The deployer's own settings sit in its
// optional sub-object.
type ApplyAssignment struct {
	Deployer  DeployerKind `json:"deployer" enum:"helm"`
	Namespace string       `json:"namespace"`
	// Definition is the artifact that holds the deployment definition.
	Definition AssignmentArtifact `json:"definition"`
	// Overlays are the chart's values files, applied in order.
	Overlays []string `json:"overlays"`
	// Images are the images to run, by the Application's artifact name.
	Images map[string]AssignmentImage `json:"images"`
	// Variables are the winning values for this Target. Secrets are fetched
	// by the agent, not carried here.
	Variables []AssignmentVariable `json:"variables"`
	Secrets   []AssignmentSecret   `json:"secrets"`
	// Helm is set when Deployer is helm.
	Helm *HelmApply `json:"helm,omitempty"`
	// Reason says why the Deployment was requested.
	Reason DeploymentReason `json:"reason" enum:"rollout,redeploy,rollback"`
	// Restores is the Deployment a rollback replays, when Reason is rollback.
	Restores *string `json:"restores,omitempty"`
}

// AssignmentImage is one image an executed Deployment runs.
type AssignmentImage struct {
	ArtifactID string `json:"artifact_id"`
	// Repository is where the image is published, without a tag or digest.
	Repository string `json:"repository"`
	Digest     string `json:"digest"`
	// Image is Repository@Digest, the form a chart receives.
	Image string `json:"image"`
}

// HelmApply is the Helm deployer's settings.
type HelmApply struct {
	// ReleaseName is the Helm release the agent installs or upgrades.
	ReleaseName string `json:"release_name"`
}

// VariableSource says which owner's value won.
type VariableSource string

const (
	VariableSourceProject     VariableSource = "project"
	VariableSourceApplication VariableSource = "application"
)

// VariableKind is what a variable carries.
type VariableKind string

const (
	VariableKindText   VariableKind = "text"
	VariableKindSecret VariableKind = "secret"
)

// VariableDeliverAs says how a secret reaches the chart.
type VariableDeliverAs string

const (
	VariableDeliverAsSecret VariableDeliverAs = "secret"
	VariableDeliverAsValue  VariableDeliverAs = "value"
)

// AssignmentVariable is one winning value for the Deployment's Target.
type AssignmentVariable struct {
	Name   string         `json:"name"`
	Source VariableSource `json:"source" enum:"project,application"`
	Kind   VariableKind   `json:"kind" enum:"text,secret"`
	// ValuesPath is where the chart receives it, when it has a path.
	ValuesPath string            `json:"values_path,omitempty"`
	DeliverAs  VariableDeliverAs `json:"deliver_as,omitempty" enum:"secret,value"`
	// Value is a text value, which may contain bindings.
	Value string `json:"value"`
}

// AssignmentSecret is one secret variable the agent resolves.
type AssignmentSecret struct {
	// Name is the variable's name and the Kubernetes Secret key it becomes.
	Name       string            `json:"name"`
	Source     VariableSource    `json:"source" enum:"project,application"`
	ValuesPath string            `json:"values_path,omitempty"`
	DeliverAs  VariableDeliverAs `json:"deliver_as" enum:"secret,value"`
	Store      StoreSettings     `json:"store"`
	Ref        SecretRef         `json:"ref"`
	// Fingerprint pins a value a rollback replays.
	Fingerprint string `json:"fingerprint,omitempty"`
}

// StoreKind names a secret manager the agent can read.
type StoreKind string

const (
	StoreKubernetes        StoreKind = "kubernetes"
	StoreAWSSecretsManager StoreKind = "aws_secrets_manager"
	StoreVault             StoreKind = "vault"
	StoreDoppler           StoreKind = "doppler"
)

// StoreSettings is a secret store's non-secret settings for one Deployment.
// It never carries a credential: the agent authenticates with its own
// identity, or reads a credential the customer put in its namespace.
type StoreSettings struct {
	ID         string           `json:"id"`
	Kind       StoreKind        `json:"kind" enum:"kubernetes,aws_secrets_manager,vault,doppler"`
	Kubernetes *KubernetesStore `json:"kubernetes,omitempty"`
	AWS        *AWSStore        `json:"aws,omitempty"`
	Vault      *VaultStore      `json:"vault,omitempty"`
	Doppler    *DopplerStore    `json:"doppler,omitempty"`
}

// KubernetesStore reads a Secret in the Target's namespace.
type KubernetesStore struct{}

type AWSStore struct {
	Region string `json:"region"`
	// RoleARN is assumed on top of the agent's Pod identity.
	RoleARN string `json:"role_arn,omitempty"`
}

type VaultStore struct {
	Address  string `json:"address"`
	Mount    string `json:"mount"`
	AuthRole string `json:"auth_role"`
	// AuthMount defaults to "kubernetes".
	AuthMount string `json:"auth_mount,omitempty"`
	// Namespace is a Vault Enterprise namespace.
	Namespace string `json:"namespace,omitempty"`
}

type DopplerStore struct {
	// TokenSecretName names the Secret holding the Doppler token, in the
	// agent's namespace.
	TokenSecretName string `json:"token_secret_name"`
	TokenSecretKey  string `json:"token_secret_key"`
}

// SecretRef is where a secret is, and which version to fetch.
type SecretRef struct {
	// Path is a Secret name, AWS secret ID or ARN, Vault path, or Doppler name.
	Path string `json:"path"`
	// Field is the key inside a map or JSON secret.
	Field string `json:"field,omitempty"`
	// Version is the provider's version; empty means the latest.
	Version string `json:"version,omitempty"`
}
