FROM --platform=$BUILDPLATFORM golang:1.25-bookworm AS builder

ARG TARGETOS=linux
ARG TARGETARCH
ARG VERSION=dev
ARG COMMIT=none
ARG BUILD_DATE=unknown

WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build \
    -ldflags="-s -w \
      -X github.com/randybias/tentacular-mcp/pkg/version.Version=${VERSION} \
      -X github.com/randybias/tentacular-mcp/pkg/version.Commit=${COMMIT} \
      -X github.com/randybias/tentacular-mcp/pkg/version.Date=${BUILD_DATE}" \
    -o tentacular-mcp ./cmd/tentacular-mcp

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /build/tentacular-mcp /tentacular-mcp

USER nonroot:nonroot

EXPOSE 8080

ENTRYPOINT ["/tentacular-mcp"]
