# Build the manager binary
FROM golang:1.13 as builder

WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum
# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN go mod download

# Copy the go source
COPY main.go main.go
COPY api/ api/
COPY controllers/ controllers/
COPY pkg/ pkg/
COPY internal/ internal/
COPY hack/pluginbuilder.sh pluginbuilder.sh

# Build
RUN CGO_ENABLED=1 GOOS=linux GOARCH=amd64 GO111MODULE=on go build -a -o manager main.go
RUN ./pluginbuilder.sh

# Use distroless as minimal base image to package the manager binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
# FROM gcr.io/distroless/static:nonroot
FROM ubuntu:xenial
RUN apt-get update && apt-get upgrade
RUN apt-get install -y git
RUN useradd -m -d /home/nonroot nonroot
WORKDIR /
COPY --from=builder /workspace/manager .
COPY --from=builder /workspace/internal/bin/ /etc/slipway/
USER nonroot:nonroot

ENTRYPOINT ["/manager"]
