package helm

import (
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// restGetter gives Helm the agent's own Kubernetes configuration, scoped to
// one namespace, with no kubeconfig on disk.
type restGetter struct {
	config    *rest.Config
	namespace string
	discovery discovery.CachedDiscoveryInterface
	mapper    meta.RESTMapper
}

func newRESTGetter(config *rest.Config, disc discovery.CachedDiscoveryInterface, mapper meta.RESTMapper, namespace string) *restGetter {
	return &restGetter{config: config, namespace: namespace, discovery: disc, mapper: mapper}
}

// newDiscovery returns the cached discovery client and REST mapper shared by
// every apply.
func newDiscovery(config *rest.Config) (discovery.CachedDiscoveryInterface, meta.RESTMapper, error) {
	client, err := discovery.NewDiscoveryClientForConfig(config)
	if err != nil {
		return nil, nil, err
	}
	cached := memory.NewMemCacheClient(client)
	return cached, restmapper.NewDeferredDiscoveryRESTMapper(cached), nil
}

func (g *restGetter) ToRESTConfig() (*rest.Config, error) { return rest.CopyConfig(g.config), nil }

func (g *restGetter) ToDiscoveryClient() (discovery.CachedDiscoveryInterface, error) {
	return g.discovery, nil
}

func (g *restGetter) ToRESTMapper() (meta.RESTMapper, error) { return g.mapper, nil }

func (g *restGetter) ToRawKubeConfigLoader() clientcmd.ClientConfig {
	return namespaceConfig{getter: g}
}

// namespaceConfig is the clientcmd.ClientConfig Helm reads the namespace
// and REST configuration from.
type namespaceConfig struct{ getter *restGetter }

func (c namespaceConfig) RawConfig() (clientcmdapi.Config, error) { return clientcmdapi.Config{}, nil }
func (c namespaceConfig) ClientConfig() (*rest.Config, error)     { return c.getter.ToRESTConfig() }
func (c namespaceConfig) Namespace() (string, bool, error)        { return c.getter.namespace, true, nil }
func (c namespaceConfig) ConfigAccess() clientcmd.ConfigAccess    { return nil }
