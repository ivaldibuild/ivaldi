# The Kubernetes agent, ivaldi-k8s-agent.
#
# Build it from the repository root, because it uses pkg/ through go.work:
#
#   docker build -f apps/k8s-agent/Dockerfile --build-arg VERSION=$(scripts/version.sh) -t ivaldi-k8s-agent .

FROM golang:1.27.1-alpine AS build

WORKDIR /src

# Every module in go.work must be present, even the ones this binary does not use.
COPY go.work go.work.sum ./
COPY pkg/go.mod pkg/go.sum pkg/
COPY apps/cli/go.mod apps/cli/go.sum apps/cli/
COPY apps/control-plane/go.mod apps/control-plane/go.sum apps/control-plane/
COPY apps/k8s-agent/go.mod apps/k8s-agent/go.sum apps/k8s-agent/

RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY pkg pkg

COPY apps/k8s-agent apps/k8s-agent

ARG VERSION=dev

RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
	CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.VERSION=${VERSION}" -o /out/ivaldi-k8s-agent ./apps/k8s-agent/cmd

# nonroot is UID 65532, which the chart's securityContext runs as.
FROM gcr.io/distroless/static-debian12:nonroot AS release

COPY --from=build /out/ivaldi-k8s-agent /usr/local/bin/ivaldi-k8s-agent

# Liveness and readiness, for the kubelet only.
EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/ivaldi-k8s-agent"]
