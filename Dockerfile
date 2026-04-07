# syntax=docker/dockerfile:1.7
ARG GO_VERSION=1.25

FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-bookworm AS build
WORKDIR /src
ENV CGO_ENABLED=0 GO111MODULE=on

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/root/go/pkg/mod \
    go mod download && go mod verify

COPY . .

ARG TARGETOS TARGETARCH
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/root/go/pkg/mod \
    GOOS=$TARGETOS GOARCH=$TARGETARCH go build \
      -trimpath -ldflags="-s -w" -o /out/agn ./cmd/agn

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
LABEL org.opencontainers.image.source="https://github.com/agynio/agn-cli"
COPY --from=build /out/agn ./agn
ENTRYPOINT ["/app/agn"]
