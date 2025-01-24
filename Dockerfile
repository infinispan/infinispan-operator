FROM registry.access.redhat.com/ubi9/go-toolset:1.22.9 AS build
ARG OPERATOR_VERSION
ARG SKAFFOLD_GO_GCFLAGS
WORKDIR /workspace
USER root
# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum
# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN go mod download

# $rootfs will contain the libraries to manually export to the runtime container
ENV rootfs=/tmp/rootfs
RUN dnf install -y --installroot $rootfs --setopt install_weak_deps=false --nodocs openssl && \
    dnf --installroot $rootfs clean all && \
    rm -rf $rootfs/var/cache/* $rootfs/var/log/dnf* $rootfs/var/log/yum.*

# Copy the go source
COPY main.go main.go
COPY api/ api/
COPY controllers/ controllers/
COPY launcher/ launcher/
COPY pkg/ pkg/

RUN CGO_ENABLED=1 GOOS=linux GO111MODULE=on \
    go build -a -o /bin/infinispan-operator \
    -ldflags="-X 'github.com/infinispan/infinispan-operator/launcher.Version=${OPERATOR_VERSION}'" -gcflags="${SKAFFOLD_GO_GCFLAGS}" main.go

FROM registry.access.redhat.com/ubi9/ubi-micro
COPY --from=build /bin/infinispan-operator /usr/local/bin/infinispan-operator
# Open SSL library is not directly used by our operator, 
# but it may be invoked by OCP using FIPS with GoLang 1.22
COPY --from=build /tmp/rootfs/usr/bin/openssl /usr/bin/openssl
COPY --from=build /tmp/rootfs/lib64 /lib64
# Define GOTRACEBACK to mark this container as using the Go language runtime
# for `skaffold debug` (https://skaffold.dev/docs/workflows/debug/).
ENV GOTRACEBACK=single
ENTRYPOINT [ "/usr/local/bin/infinispan-operator" ]
