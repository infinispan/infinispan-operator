FROM registry.access.redhat.com/ubi8/go-toolset:1.15.14 AS build
ARG OPERATOR_VERSION
WORKDIR /workspace
USER root
COPY go.mod go.sum ./
RUN go mod tidy
COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GO111MODULE=on go build -a -o /bin/infinispan-operator \
    -ldflags="-X 'github.com/infinispan/infinispan-operator/launcher.Version=${OPERATOR_VERSION}'" main.go

FROM registry.access.redhat.com/ubi8/ubi-minimal
COPY --from=build /bin/infinispan-operator /usr/local/bin/infinispan-operator
ENTRYPOINT [ "infinispan-operator" ]
