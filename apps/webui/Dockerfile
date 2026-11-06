# The Ivaldi web app, served by nginx on port 8080 as an unprivileged user.
#
#   docker build -t ivaldi-webui apps/webui
#   docker run -p 8080:8080 -e IVALDI_API_URL=https://ivaldi.example.com ivaldi-webui
#
# Leave IVALDI_API_URL unset when a reverse proxy serves the web app and the
# control plane from one origin (see docs/self-hosting/web-app.md).

FROM node:24-alpine AS build
WORKDIR /src
RUN corepack enable
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
# pnpm refuses to install without a directory to link the agent skills that
# pnpm-workspace.yaml approves; the image has no use for them.
RUN mkdir -p .claude/skills && pnpm install --frozen-lockfile
COPY . .
RUN pnpm build

FROM nginxinc/nginx-unprivileged:1.29-alpine
COPY docker/nginx.conf /etc/nginx/conf.d/default.conf
COPY --chmod=755 docker/40-ivaldi-api-url.sh /docker-entrypoint.d/
COPY --from=build --chown=101:0 --chmod=g+w /src/dist/webui/browser /usr/share/nginx/html
EXPOSE 8080
