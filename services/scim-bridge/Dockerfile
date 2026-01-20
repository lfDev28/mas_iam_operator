# syntax=docker/dockerfile:1

ARG GO_VERSION=1.22

FROM golang:${GO_VERSION}-alpine AS builder
WORKDIR /workspace
RUN apk add --no-cache ca-certificates
COPY . .
ARG TARGETOS=linux
ARG TARGETARCH=amd64
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -o /out/scim-bridge ./cmd/scim-bridge

FROM gcr.io/distroless/static:nonroot
COPY --from=builder /out/scim-bridge /usr/local/bin/scim-bridge
ENTRYPOINT ["/usr/local/bin/scim-bridge"]
