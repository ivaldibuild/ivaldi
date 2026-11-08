# The ivaldi binary: the control plane, gateway, worker, and indexer.
#
# Build it from the repository root, because it uses pkg/ through go.work:
#
#   docker build -f apps/control-plane/Dockerfile --build-arg VERSION=$(git describe --tags --always) -t ivaldi .
#   docker run ivaldi serve

FROM golang:1.27.1-alpine AS build

WORKDIR /src

# Every module in go.work must be present, even the ones this binary does not use.
COPY go.work go.work.sum ./
COPY pkg/go.mod pkg/go.sum pkg/
COPY apps/cli/go.mod apps/cli/go.sum apps/cli/
COPY apps/control-plane/go.mod apps/control-plane/go.sum apps/control-plane/
COPY apps/k8s-agent/go.mod apps/k8s-agent/

RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY pkg pkg

COPY apps/control-plane apps/control-plane

ARG VERSION=dev

RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
	CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.VERSION=${VERSION}" -o /out/ivaldi ./apps/control-plane/cmd

FROM gcr.io/distroless/static-debian12:nonroot AS release

COPY --from=build /out/ivaldi /usr/local/bin/ivaldi

# 8660 is the API, 8661 the runner endpoint, and 8662 health and metrics.
EXPOSE 8660 8661 8662

ENTRYPOINT ["/usr/local/bin/ivaldi"]

CMD ["serve"]
