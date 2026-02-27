FROM golang:1.25-bookworm AS builder

ARG TARGETOS=linux
ARG TARGETARCH=amd64

WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -ldflags="-s -w" -o tentacular-mcp ./cmd/tentacular-mcp

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /build/tentacular-mcp /tentacular-mcp

USER nonroot:nonroot

EXPOSE 8080

ENTRYPOINT ["/tentacular-mcp"]
