# syntax=docker/dockerfile:1

# The build stage runs on the build host and cross-compiles for the target,
# so arm64 images do not need QEMU.
FROM --platform=$BUILDPLATFORM golang:1.26 AS build
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" \
    -o /out/ebot-web ./cmd/server

FROM gcr.io/distroless/static:nonroot

ARG VERSION=dev
ARG SOURCE_URL=
LABEL org.opencontainers.image.title="ebot-web" \
      org.opencontainers.image.description="eBot CS:GO match panel" \
      org.opencontainers.image.source="${SOURCE_URL}" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.licenses="GPL-3.0-or-later"

COPY --from=build /out/ebot-web /ebot-web

# Distroless has no shell, so there is no HEALTHCHECK. Use the /healthz and
# /readyz probes from the orchestrator instead.
EXPOSE 8080
USER 65532:65532
ENTRYPOINT ["/ebot-web"]
