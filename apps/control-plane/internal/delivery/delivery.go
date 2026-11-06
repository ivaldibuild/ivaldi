// Package delivery is the control plane's delivery capability (ADR 0006,
// docs/design/delivery-data-model.md): where software runs, how Releases
// move through Environments, and what gets deployed.
//
// So far it covers the configuration side: Environments and their Targets,
// Lifecycles, Registries, and Applications with their artifacts, Channels,
// and placements on Targets. Builds, Releases, and Deployments follow.
package delivery

import "github.com/ivaldibuild/ivaldi/pkg/id"

func init() {
	id.MustRegister(Environment{}.Prefix(), "delivery.Environment")
	id.MustRegister(Lifecycle{}.Prefix(), "delivery.Lifecycle")
	id.MustRegister(LifecyclePhase{}.Prefix(), "delivery.LifecyclePhase")
	id.MustRegister(Target{}.Prefix(), "delivery.Target")
	id.MustRegister(Registry{}.Prefix(), "delivery.Registry")
	id.MustRegister(Application{}.Prefix(), "delivery.Application")
	id.MustRegister(Artifact{}.Prefix(), "delivery.Artifact")
	id.MustRegister(Channel{}.Prefix(), "delivery.Channel")
	id.MustRegister(ChannelRule{}.Prefix(), "delivery.ChannelRule")
	id.MustRegister(Build{}.Prefix(), "delivery.Build")
	id.MustRegister(Release{}.Prefix(), "delivery.Release")
}
