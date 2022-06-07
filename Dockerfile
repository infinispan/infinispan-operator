FROM registry.access.redhat.com/ubi9/go-toolset:1.17.7 AS build
ARG OPERATOR_VERSION
WORKDIR /workspace
USER root
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
COPY launcher/ launcher/
COPY pkg/ pkg/

RUN GOPROXY="https://proxy.golang.org,direct" CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GO111MODULE=on \
    go build -a -o /bin/infinispan-operator \
    -ldflags="-X 'github.com/infinispan/infinispan-operator/launcher.Version=${OPERATOR_VERSION}'" main.go

FROM registry.access.redhat.com/ubi9/ubi-minimal
COPY --from=build /bin/infinispan-operator /usr/local/bin/infinispan-operator
ENTRYPOINT [ "infinispan-operator" ]
