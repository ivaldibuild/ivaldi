package releases

import (
	"time"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/delivery"

	"github.com/ivaldibuild/ivaldi/pkg/optional"
)

type ReportBuildRequest struct {
	ApplicationIdentifier string `path:"application" doc:"The Application's ID, or the slug of an active Application"`
	Body                  BuildBody
}

type CreateReleaseRequest struct {
	ApplicationIdentifier string `path:"application" doc:"The Application's ID, or the slug of an active Application"`
	Body                  struct {
		Version          string      `json:"version" required:"true" minLength:"1" maxLength:"255" doc:"The Release's version, unique within the Application, such as 1.4.0"`
		ChannelID        string      `json:"channel_id,omitempty" required:"false" doc:"The Channel to join; when omitted, the one active Channel whose rules the Release meets"`
		Builds           []BuildBody `json:"builds" required:"true" minItems:"1" doc:"One Build per artifact"`
		Publish          bool        `json:"publish,omitempty" required:"false" doc:"Publish the Release as soon as it is created"`
		TriggeredBy      string      `json:"triggered_by,omitempty" required:"false" maxLength:"255" doc:"Who triggered the Release, such as the CI user who ran the pipeline; recorded as reported, not verified"`
		TriggeredByEmail string      `json:"triggered_by_email,omitempty" required:"false" maxLength:"320" doc:"The email address of whoever triggered the Release; recorded as reported, not verified"`
	}
}

type ListReleasesRequest struct {
	ApplicationIdentifier string `path:"application" doc:"The Application's ID, or the slug of an active Application"`
	ChannelID             string `query:"channel_id" doc:"Keep the Releases in this Channel"`
	Status                string `query:"status" enum:"draft,published,withdrawn" doc:"Keep the Releases with this status"`
	Limit                 int    `query:"limit" minimum:"1" maximum:"100" doc:"Most items to return"`
	SortBy                string `query:"sort_by" enum:"created_at,version" doc:"Field to sort by: created_at (the default) or version, as text"`
	SortOrder             string `query:"sort_order" enum:"asc,desc" doc:"Sort direction"`
	NextCursor            string `query:"next_cursor" doc:"Cursor from a previous page's next_cursor"`
	PreviousCursor        string `query:"previous_cursor" doc:"Cursor from a previous page's previous_cursor"`
}

type ReleaseRequest struct {
	ReleaseID string `path:"release_id" doc:"The Release"`
}

type ReleaseVariablesRequest struct {
	ReleaseID string `path:"release_id" doc:"The Release"`
	TargetID  string `query:"target_id" doc:"A Target, to also get the value of each variable a Deployment there would get"`
	// DeploymentID asks for what a rollback deploys rather than a rollout.
	DeploymentID string `query:"deployment_id" doc:"One of the Release's Deployments, to get the revision it used rather than the current one: what a rollback that restores it deploys"`
}

type RefreshReleaseVariablesRequest struct {
	ReleaseID string `path:"release_id" doc:"The Release"`
	Preview   bool   `query:"preview" doc:"Only say what would change, without taking a revision"`
}

// BuildBody names one artifact's Build and carries what CI knows about it.
type BuildBody struct {
	Artifact string `json:"artifact" required:"true" minLength:"1" doc:"The name of the Application's artifact, such as api"`
	Digest   string `json:"digest,omitempty" required:"false" doc:"The content digest, such as sha256:<64 hex characters>"`
	Tag      string `json:"tag,omitempty" required:"false" doc:"A tag to resolve to a digest through the artifact's Registry; not supported yet, give the digest"`
	Version  string `json:"version,omitempty" required:"false" doc:"The version CI gave the Build"`
	SourceID string `json:"source_id,omitempty" required:"false" doc:"The Source it was built from"`
	// CommitSHA and SourceRef are what Channel rules and change history use.
	CommitSHA  string     `json:"commit_sha,omitempty" required:"false" doc:"The commit it was built from"`
	SourceRef  string     `json:"source_ref,omitempty" required:"false" doc:"The full Git ref it was built from, such as refs/tags/v1.4.0 or refs/pull/42/head"`
	CIProvider string     `json:"ci_provider,omitempty" required:"false" doc:"The CI system, such as github_actions"`
	CIRunRef   string     `json:"ci_run_ref,omitempty" required:"false" doc:"The CI run, such as its URL"`
	BuiltAt    *time.Time `json:"built_at,omitempty" required:"false" doc:"When it was built"`
	Tags       []string   `json:"tags,omitempty" required:"false" doc:"Tags pushed with the digest, as hints"`
}

func (b BuildBody) input() delivery.BuildInput {
	return delivery.BuildInput{
		Artifact: b.Artifact, Digest: b.Digest, Tag: b.Tag,
		Details: delivery.BuildDetails{
			Version: optional.Of(b.Version), SourceID: optional.Of(b.SourceID), CommitSHA: optional.Of(b.CommitSHA),
			SourceRef: optional.Of(b.SourceRef), CIProvider: optional.Of(b.CIProvider), CIRunRef: optional.Of(b.CIRunRef),
			ReportedTags: b.Tags, BuiltAt: b.BuiltAt,
		},
	}
}
