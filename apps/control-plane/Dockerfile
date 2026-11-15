# The ivaldi binary: the control plane, gateway, worker, and indexer. The
# control plane also serves the web app, so this image is the whole product.
#
# Build it from the repository root, because it uses pkg/ through go.work and
# the web app's sources:
#
#   docker build -f apps/control-plane/Dockerfile --build-arg VERSION=$(scripts/version.sh ivaldi) -t ivaldi .
#   docker run ivaldi serve

# The web app, built first because the Go build embeds its output.
FROM node:24-alpine AS web

WORKDIR /src/apps/webui

RUN corepack enable

COPY apps/webui/package.json apps/webui/pnpm-lock.yaml apps/webui/pnpm-workspace.yaml ./
# pnpm refuses to install without a directory to link the agent skills that
# pnpm-workspace.yaml approves; the build has no use for them.
RUN mkdir -p .claude/skills && pnpm install --frozen-lockfile

COPY apps/webui .
RUN pnpm build

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

COPY apps/control-plane apps/control-plane

# The web app's output lands where //go:embed expects it (internal/webui), and
# -tags webui is what makes the build include it. A build without the tag, such
# as `go build` or `go test`, needs no web app at all.
COPY --from=web /src/apps/webui/dist/webui/browser apps/control-plane/internal/webui/dist

ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILDTIME=unknown
# The released agent chart this build's onboarding guide offers.
ARG AGENT_CHART_VERSION=0.0.0

RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
	CGO_ENABLED=0 go build -trimpath -tags webui \
	-ldflags "-s -w -X main.VERSION=${VERSION} -X main.COMMIT=${COMMIT} -X main.BUILDTIME=${BUILDTIME} \
	-X github.com/ivaldibuild/ivaldi-control-plane/cmd/server.AGENT_CHART_VERSION=${AGENT_CHART_VERSION}" \
	-o /out/ivaldi ./apps/control-plane/cmd

FROM gcr.io/distroless/static-debian12:nonroot AS release

COPY --from=build /out/ivaldi /usr/local/bin/ivaldi

# 8660 is the API and the web app, 8661 the runner endpoint, and 8662 health
# and metrics.
EXPOSE 8660 8661 8662

ENTRYPOINT ["/usr/local/bin/ivaldi"]

CMD ["serve"]
