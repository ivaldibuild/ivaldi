#!/usr/bin/env bash
# The Kubernetes agent's end-to-end test (make k8s_agent_e2e).
#
# It starts, on the kind Docker network:
#   - a kind cluster,
#   - a plain-HTTP OCI registry holding the demo chart and two demo images,
#   - PostgreSQL, Valkey, and the control plane, migrated and bootstrapped,
# loads the agent's image into the cluster, and runs the e2e Go test, which
# drives everything else through the control plane's API.
#
# Needs docker, kind, helm, curl, and openssl. Set IVALDI_E2E_KEEP=1 to keep
# everything running afterwards; run it again to start over.
set -euo pipefail

root=$(cd "$(dirname "$0")/../../.." && pwd)
here="$root/apps/k8s-agent/e2e"

# config.yml names these containers.
name=ivaldi-e2e
network=kind
registry="$name-registry"
postgres="$name-postgres"
valkey="$name-valkey"
ivaldi="$name-ivaldi"
# config.yml names this port in external_url.
api_port=18660
registry_port=${IVALDI_E2E_REGISTRY_PORT:-15000}

work=$(mktemp -d)
export KUBECONFIG="$work/kubeconfig"

log() { printf '\n==> %s\n' "$*"; }

cleanup() {
	status=$?
	if [ "$status" -ne 0 ]; then
		log "Failed; the control plane's and the agent's logs follow"
		docker logs --tail 200 "$ivaldi" 2>&1 || true
		kubectl_node logs -n ivaldi-agent deployment/ivaldi-agent --tail 200 2>&1 || true
		kubectl_node get all -A 2>&1 || true
	fi
	if [ "${IVALDI_E2E_KEEP:-}" = 1 ]; then
		log "Kept: KUBECONFIG=$KUBECONFIG, API http://127.0.0.1:$api_port, admin password in $work/admin-password"
	else
		teardown
		rm -rf "$work"
	fi
	exit "$status"
}

teardown() {
	kind delete cluster --name "$name" >/dev/null 2>&1 || true
	docker rm -f "$registry" "$postgres" "$valkey" "$ivaldi" >/dev/null 2>&1 || true
}

# kubectl from the kind node, so the host needs none.
kubectl_node() { docker exec "$name-control-plane" kubectl --kubeconfig /etc/kubernetes/admin.conf "$@"; }

# digest prints the manifest digest the registry serves for repository:tag.
digest() {
	curl -fsSI \
		-H 'Accept: application/vnd.oci.image.index.v1+json, application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.list.v2+json, application/vnd.docker.distribution.manifest.v2+json' \
		"http://127.0.0.1:$registry_port/v2/$1/manifests/$2" |
		tr -d '\r' | sed -n 's/^[Dd]ocker-[Cc]ontent-[Dd]igest: //p'
}

wait_for() {
	local what=$1
	shift
	for _ in $(seq 1 60); do
		if "$@" >/dev/null 2>&1; then return 0; fi
		sleep 2
	done
	echo "timed out waiting for $what" >&2
	return 1
}

teardown
trap cleanup EXIT

log "Creating the kind cluster"
kind create cluster --name "$name" --config "$here/kind.yaml" --kubeconfig "$KUBECONFIG" --wait 120s

log "Starting the registry, PostgreSQL, and Valkey"
docker run -d --name "$registry" --network "$network" -p "127.0.0.1:$registry_port:5000" registry:3 >/dev/null
docker run -d --name "$postgres" --network "$network" \
	-e POSTGRES_USER=ivaldi -e POSTGRES_PASSWORD=ivaldi -e POSTGRES_DB=ivaldi postgres:17.5-alpine3.22 >/dev/null
docker run -d --name "$valkey" --network "$network" valkey/valkey:8.1-alpine3.22 >/dev/null

# Every node pulls from the registry over plain HTTP, by its name on the
# kind network.
for node in $(kind get nodes --name "$name"); do
	dir="/etc/containerd/certs.d/$registry:5000"
	docker exec "$node" mkdir -p "$dir"
	printf '[host."http://%s:5000"]\n  capabilities = ["pull", "resolve"]\n' "$registry" |
		docker exec -i "$node" cp /dev/stdin "$dir/hosts.toml"
done

log "Building the images"
version=$("$root/scripts/version.sh")
docker build -q -f "$root/apps/control-plane/Dockerfile" --build-arg VERSION="$version" -t ivaldi:e2e "$root" >/dev/null &
control_plane_build=$!
docker build -q -f "$root/apps/k8s-agent/Dockerfile" --build-arg VERSION="$version" -t ivaldi-k8s-agent:e2e "$root" >/dev/null
wait "$control_plane_build"
kind load docker-image ivaldi-k8s-agent:e2e --name "$name"

log "Pushing the demo chart and images"
wait_for "the registry" curl -fs "http://127.0.0.1:$registry_port/v2/"
# Two builds of one image that differ only in a label, so each has its own
# digest. pause runs until stopped and is ready as soon as it starts.
for build in 1 2; do
	printf 'FROM registry.k8s.io/pause:3.10\nLABEL build.ivaldi.e2e=%s\n' "$build" |
		docker build -q -t "127.0.0.1:$registry_port/demo:$build" - >/dev/null
	docker push -q "127.0.0.1:$registry_port/demo:$build" >/dev/null
done
helm package "$here/testdata/demo" --destination "$work" >/dev/null
helm push "$work/demo-0.1.0.tgz" "oci://127.0.0.1:$registry_port/charts" --plain-http >/dev/null 2>&1

image1=$(digest demo 1)
image2=$(digest demo 2)
chart=$(digest charts/demo 0.1.0)
[ -n "$image1" ] && [ -n "$image2" ] && [ -n "$chart" ] || { echo "could not read the pushed digests" >&2; exit 1; }

log "Migrating the database"
wait_for "PostgreSQL" docker exec "$postgres" pg_isready -U ivaldi -d ivaldi
docker run --rm --network "$network" -v "$root/apps/control-plane/internal:/capabilities:ro" \
	--entrypoint /bin/sh migrate/migrate:v4.20.1 -ec '
for dir in /capabilities/*/migrations; do
	ls "$dir"/*.up.sql >/dev/null 2>&1 || continue
	capability=$(basename "$(dirname "$dir")")
	table=$capability
	[ "$capability" = versioncontrol ] && table=version_control
	migrate -path "$dir" -database "postgres://ivaldi:ivaldi@'"$postgres"':5432/ivaldi?sslmode=disable&x-migrations-table=${table}_schema_migrations" up
done' >/dev/null

log "Starting the control plane"
mkdir -p "$work/secrets"
openssl genpkey -algorithm EC -pkeyopt ec_paramgen_curve:P-256 -out "$work/secrets/signing.pem" 2>/dev/null
openssl rand -hex 16 >"$work/secrets/admin-password"
cp "$work/secrets/admin-password" "$work/admin-password"
# The image runs as nonroot (65532); these are throwaway.
chmod 755 "$work/secrets" && chmod 644 "$work/secrets"/*
docker run -d --name "$ivaldi" --network "$network" -p "127.0.0.1:$api_port:8660" \
	-v "$here/config.yml:/e2e-config.yml:ro" -v "$work/secrets:/e2e:ro" \
	ivaldi:e2e serve --config /e2e-config.yml >/dev/null
wait_for "the control plane" curl -fs "http://127.0.0.1:$api_port/healthz"

log "Running the test"
cd "$root"
IVALDI_E2E_API_URL="http://127.0.0.1:$api_port" \
	IVALDI_E2E_CLUSTER_API_URL="http://$ivaldi:8660" \
	IVALDI_E2E_ADMIN_EMAIL=admin@e2e.ivaldi.test \
	IVALDI_E2E_ADMIN_PASSWORD="$(cat "$work/admin-password")" \
	IVALDI_E2E_REGISTRY="$registry:5000" \
	IVALDI_E2E_CHART_DIGEST="$chart" \
	IVALDI_E2E_IMAGE_DIGESTS="$image1,$image2" \
	IVALDI_E2E_AGENT_IMAGE=ivaldi-k8s-agent:e2e \
	IVALDI_E2E_AGENT_CHART="$root/deploy/helm/ivaldi-agent" \
	go test -tags e2e -count=1 -v -timeout 15m ./apps/k8s-agent/e2e/
