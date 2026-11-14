package helm

import (
	"bytes"
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/kustomize/kyaml/kio"
	kyaml "sigs.k8s.io/kustomize/kyaml/yaml"

	"github.com/ivaldibuild/ivaldi-k8s-agent/internal/deploy"
)

// podTemplatePaths are where each workload kind keeps its Pod template's
// metadata.
var podTemplatePaths = map[string][]string{
	"Deployment":  {"spec", "template", "metadata"},
	"StatefulSet": {"spec", "template", "metadata"},
	"DaemonSet":   {"spec", "template", "metadata"},
	"ReplicaSet":  {"spec", "template", "metadata"},
	"Job":         {"spec", "template", "metadata"},
	"CronJob":     {"spec", "jobTemplate", "spec", "template", "metadata"},
}

// postRenderer labels and annotates what a chart renders, hooks included,
// so the agent can find it and tell which Deployment applied it. It refuses
// cluster-scoped objects, because the agent is never granted cluster-wide
// writes, and objects in any namespace but the Target's.
type postRenderer struct {
	applicationID string
	deploymentID  string
	releaseID     string
	// namespace is the Target's: every object must be in it.
	namespace string
	// mapper tells cluster-scoped kinds apart; nil skips the check.
	mapper meta.RESTMapper
}

func (p *postRenderer) Run(rendered *bytes.Buffer) (*bytes.Buffer, error) {
	nodes, err := kio.ParseAll(rendered.String())
	if err != nil {
		return nil, fmt.Errorf("parse rendered manifests: %w", err)
	}
	for _, node := range nodes {
		if err := p.checkScope(node); err != nil {
			return nil, err
		}
		// An explicit namespace would let one Target's Deployment change
		// another namespace the agent may write to. Objects without one are
		// put in the release namespace, the Target's, by Helm.
		if ns := node.GetNamespace(); ns != "" && ns != p.namespace {
			return nil, deploy.Failf("the chart puts %s %s in namespace %s; this Deployment may only change %s",
				node.GetKind(), node.GetName(), ns, p.namespace)
		}
		// Each filter runs on its own: PipeE hands a filter's result to
		// the next.
		for _, filter := range []kyaml.Filter{
			kyaml.SetAnnotation(deploy.AnnotationDeployment, p.deploymentID),
			kyaml.SetAnnotation(deploy.AnnotationRelease, p.releaseID),
		} {
			if err := node.PipeE(filter); err != nil {
				return nil, err
			}
		}
		path, ok := podTemplatePaths[node.GetKind()]
		if !ok {
			continue
		}
		if err := node.PipeE(kyaml.SetLabel(deploy.LabelApplication, p.applicationID)); err != nil {
			return nil, err
		}
		template, err := node.Pipe(kyaml.LookupCreate(kyaml.MappingNode, path...))
		if err != nil {
			return nil, err
		}
		if err := template.PipeE(
			kyaml.LookupCreate(kyaml.MappingNode, "labels"),
			kyaml.SetField(deploy.LabelApplication, kyaml.NewStringRNode(p.applicationID)),
		); err != nil {
			return nil, err
		}
	}
	out, err := kio.StringAll(nodes)
	if err != nil {
		return nil, err
	}
	return bytes.NewBufferString(out), nil
}

// checkScope refuses a cluster-scoped object. A kind the API server does not
// know is left for the apply to report.
func (p *postRenderer) checkScope(node *kyaml.RNode) error {
	if p.mapper == nil {
		return nil
	}
	gv, err := schema.ParseGroupVersion(node.GetApiVersion())
	if err != nil {
		return nil
	}
	mapping, err := p.mapper.RESTMapping(schema.GroupKind{Group: gv.Group, Kind: node.GetKind()}, gv.Version)
	if err != nil {
		return nil
	}
	if mapping.Scope.Name() == meta.RESTScopeNameRoot {
		return deploy.Failf("the chart creates %s %s, which is cluster-scoped; the agent applies only namespaced objects",
			node.GetKind(), node.GetName())
	}
	return nil
}
