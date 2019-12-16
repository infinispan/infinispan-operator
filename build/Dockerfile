FROM golang:1.12 AS build
ENV GO111MODULE=on
WORKDIR /infinispan-operator
COPY go.mod go.sum ./
RUN go mod tidy
COPY . .
RUN rm -rf ./build/_output
RUN ./build/build.sh linux /bin

FROM registry.access.redhat.com/ubi8/ubi-minimal
COPY --from=build /bin/infinispan-operator /usr/local/bin/infinispan-operator
