package enrollment

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/api"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"
	"github.com/ivaldibuild/ivaldi-control-plane/internal/iam"
	"github.com/ivaldibuild/ivaldi/pkg/authorization"
	"github.com/ivaldibuild/ivaldi/pkg/identity"
)

// Controller serves Target enrolment and workload credential renewal.
type Controller struct {
	service Service
}

var _ api.Routes = (*Controller)(nil)

func NewController(service Service) *Controller { return &Controller{service: service} }

var bearer = []map[string][]string{{api.BearerAuth: {}}}

func (c *Controller) RegisterRoutes(h huma.API) {
	register(h, "create-cluster-enrolment", http.MethodPost, "/clusters/{cluster_id}/enrolments",
		"Create a single-use enrolment token for a Cluster's runtime agent", http.StatusCreated, c.Create)
	register(h, "get-cluster-agent", http.MethodGet, "/clusters/{cluster_id}/agent",
		"Get the Cluster's agent and credential status", 0, c.Status)
	register(h, "renew-workload-credential", http.MethodPost, "/agent/credentials/renew",
		"Renew the credential that authenticated the workload", 0, c.Renew)

	huma.Register(h, huma.Operation{
		OperationID:   "redeem-cluster-enrolment",
		Method:        http.MethodPost,
		Path:          "/agent/enrolments/redeem",
		Summary:       "Redeem a Cluster enrolment token for a workload credential",
		Tags:          []string{"enrolment"},
		DefaultStatus: http.StatusCreated,
		Metadata:      map[string]any{api.Public: true},
	}, c.Redeem)
}

func register[I, O any](h huma.API, id, method, path, summary string, status int, handler func(context.Context, *I) (*O, error)) {
	huma.Register(h, huma.Operation{
		OperationID: id, Method: method, Path: path, Summary: summary,
		Tags: []string{"enrolment"}, DefaultStatus: status, Security: bearer,
	}, handler)
}

type CreateRequest struct {
	ClusterID string `path:"cluster_id" doc:"The Cluster the agent will serve"`
	Body      struct {
		Method string `json:"method" required:"true" enum:"bearer,mtls,kubernetes" doc:"How the agent will authenticate"`
	}
}

func (r *CreateRequest) input(organizationID string) CreateInput {
	return CreateInput{OrganizationID: organizationID, ClusterID: r.ClusterID, Method: CredentialMethod(r.Body.Method)}
}

type ClusterRequest struct {
	ClusterID string `path:"cluster_id" doc:"The Cluster"`
}

type RedeemRequest struct {
	Body struct {
		Token           string `json:"token" required:"true" minLength:"1" doc:"The single-use enrolment token"`
		Method          string `json:"method" required:"true" enum:"bearer,mtls,kubernetes" doc:"How the agent will authenticate"`
		KubernetesToken string `json:"kubernetes_token,omitempty" required:"false" doc:"The projected service-account token, for kubernetes"`
	}
}

type RenewRequest struct {
	Body struct{}
}

type EnrolmentResponse struct {
	Body api.Response[IssuedEnrolment]
}

type RedeemedResponse struct {
	Body api.Response[Redeemed]
}

type RenewedResponse struct {
	Body api.Response[Renewed]
}

type AgentStatusResponse struct {
	Body api.Response[AgentStatus]
}

func caller(ctx context.Context) (identity.Caller, error) {
	c, ok := identity.CallerFrom(ctx)
	if !ok {
		return identity.Caller{}, huma.Error401Unauthorized("a credential is required")
	}
	return c, nil
}

func (c *Controller) Create(ctx context.Context, req *CreateRequest) (*EnrolmentResponse, error) {
	who, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	out, err := c.service.Create(ctx, who.Actor, req.input(who.OrganizationID))
	if err != nil {
		return nil, translateToHttpError(ctx, err)
	}
	return &EnrolmentResponse{Body: api.SuccessResponse(nil, *out)}, nil
}

func (c *Controller) Status(ctx context.Context, req *ClusterRequest) (*AgentStatusResponse, error) {
	who, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	out, err := c.service.Status(ctx, who.Actor, who.OrganizationID, req.ClusterID)
	if err != nil {
		return nil, translateToHttpError(ctx, err)
	}
	return &AgentStatusResponse{Body: api.SuccessResponse(nil, *out)}, nil
}

func (c *Controller) Redeem(ctx context.Context, req *RedeemRequest) (*RedeemedResponse, error) {
	in := RedeemInput{
		Token: req.Body.Token, Method: CredentialMethod(req.Body.Method),
		KubernetesToken: req.Body.KubernetesToken,
	}
	if cert := api.ClientCertificate(ctx); cert != nil {
		sum := sha256.Sum256(cert.Raw)
		in.ClientCertificateFP = hex.EncodeToString(sum[:])
		in.ClientCertificateEnds = cert.NotAfter
	}
	out, err := c.service.Redeem(ctx, in)
	if err != nil {
		return nil, translateToHttpError(ctx, err)
	}
	return &RedeemedResponse{Body: api.SuccessResponse(nil, *out)}, nil
}

func (c *Controller) Renew(ctx context.Context, _ *RenewRequest) (*RenewedResponse, error) {
	who, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	out, err := c.service.Renew(ctx, who)
	if err != nil {
		return nil, translateToHttpError(ctx, err)
	}
	return &RenewedResponse{Body: api.SuccessResponse(nil, *out)}, nil
}

func translateToHttpError(ctx context.Context, err error) error {
	var invalid *delivery.ErrInvalidInput
	var approval *authorization.ApprovalRequiredError
	switch {
	case errors.As(err, &invalid):
		return huma.Error422UnprocessableEntity(invalid.Message)
	case errors.Is(err, delivery.ErrInvalid), errors.Is(err, iam.ErrInvalid):
		return huma.Error422UnprocessableEntity("the request is invalid")
	case errors.Is(err, delivery.ErrNotFound), errors.Is(err, iam.ErrNotFound):
		return huma.Error404NotFound("not found")
	case errors.Is(err, iam.ErrAlreadyUsed):
		return huma.Error409Conflict("the enrollment token was already used")
	case errors.Is(err, iam.ErrAlreadyExists):
		return huma.Error409Conflict("that certificate or service account is already enrolled")
	case errors.Is(err, iam.ErrExpired):
		return huma.Error410Gone("the credential expired")
	case errors.Is(err, delivery.ErrArchived), errors.Is(err, iam.ErrStatus):
		return huma.Error409Conflict(err.Error())
	case errors.Is(err, delivery.ErrIdentityInUse):
		return huma.Error409Conflict("this workload identity already serves another cluster")
	case errors.Is(err, delivery.ErrNotClusterAgent):
		return huma.Error403Forbidden(err.Error())
	case errors.Is(err, identity.ErrInvalidToken):
		return huma.Error401Unauthorized("the credential is not valid")
	case errors.As(err, &approval):
		return huma.Error403Forbidden("the change needs approval; decision " + approval.DecisionID)
	case errors.Is(err, authorization.ErrDenied):
		return huma.Error403Forbidden("you are not allowed to do that")
	}
	return api.InternalError(ctx, err)
}
