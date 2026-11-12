# syntax=docker/dockerfile:1
#
# Accountedfor container image: a single static binary on a distroless base
# (no shell, no package manager), running as an unprivileged user.

ARG GO_VERSION=1.27

FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-alpine AS build
WORKDIR /src
ENV CGO_ENABLED=0 GOTOOLCHAIN=local GOFLAGS=-trimpath
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_NUMBER=0
ARG BUILD_DATE=unknown
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    GOOS=$TARGETOS GOARCH=$TARGETARCH go build \
      -ldflags "-s -w \
        -X github.com/lobojoel2003/accountedfor/internal/version.Version=${VERSION} \
        -X github.com/lobojoel2003/accountedfor/internal/version.Commit=${COMMIT} \
        -X github.com/lobojoel2003/accountedfor/internal/version.BuildNumber=${BUILD_NUMBER} \
        -X github.com/lobojoel2003/accountedfor/internal/version.BuildDate=${BUILD_DATE}" \
      -o /out/accountedfor ./cmd/accountedfor

FROM gcr.io/distroless/static-debian12:nonroot
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_DATE=unknown
LABEL org.opencontainers.image.title="Accountedfor" \
      org.opencontainers.image.description="An \"I'm safe\" board for disasters: tiny pages, SMS, PFIF import/export." \
      org.opencontainers.image.source="https://github.com/lobojoel2003/Accountedfor" \
      org.opencontainers.image.url="https://github.com/lobojoel2003/Accountedfor" \
      org.opencontainers.image.documentation="https://github.com/lobojoel2003/Accountedfor#readme" \
      org.opencontainers.image.licenses="Apache-2.0" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${COMMIT}" \
      org.opencontainers.image.created="${BUILD_DATE}"
COPY --from=build /out/accountedfor /usr/local/bin/accountedfor
USER nonroot:nonroot
ENV LISTEN_ADDR=:8080
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=30s --start-interval=2s --retries=3 \
  CMD ["/usr/local/bin/accountedfor", "healthcheck"]
ENTRYPOINT ["/usr/local/bin/accountedfor"]
CMD ["serve"]
