package secretstore

import (
	"context"
	"fmt"
	"slices"
)

// SecretGetter reads a Kubernetes Secret in the Target's namespace, returning
// its data and resourceVersion. It returns errors matching ErrNotFound,
// ErrDenied, or ErrUnavailable. The agent supplies it, so this package needs
// no Kubernetes client.
type SecretGetter func(ctx context.Context, name string) (data map[string][]byte, resourceVersion string, err error)

// NewKubernetes returns the kubernetes store over get. A Ref's Path is the
// Secret's name and its Field the key; with one key, Field may be empty. Its
// version is the Secret's resourceVersion.
func NewKubernetes(get SecretGetter) Store { return &kubernetesStore{get: get} }

type kubernetesStore struct{ get SecretGetter }

func (k *kubernetesStore) Kind() Kind     { return KindKubernetes }
func (k *kubernetesStore) Versions() bool { return true }

func (k *kubernetesStore) Get(ctx context.Context, ref Ref) (Value, error) {
	data, version, err := k.get(ctx, ref.Path)
	if err != nil {
		return Value{}, fail(KindKubernetes, ref, err)
	}
	if ref.Version != "" && ref.Version != version {
		return Value{}, fail(KindKubernetes, ref, fmt.Errorf("%w: the Secret is now at %s", ErrVersionGone, version))
	}
	key := ref.Field
	if key == "" {
		if len(data) != 1 {
			return Value{}, fail(KindKubernetes, ref, fmt.Errorf("%w: the Secret has %d keys, so the reference must name one", ErrFieldMissing, len(data)))
		}
		for only := range data {
			key = only
		}
	}
	raw, ok := data[key]
	if !ok {
		keys := make([]string, 0, len(data))
		for k := range data {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		return Value{}, fail(KindKubernetes, ref, fmt.Errorf("%w: it has %v", ErrFieldMissing, keys))
	}
	return Value{Value: string(raw), Version: version, Source: ref}, nil
}
