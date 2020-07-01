FROM golang:1.13 AS build
ENV GO111MODULE=on
WORKDIR /infinispan-operator
COPY go.mod go.sum ./
RUN go mod tidy
COPY . .
RUN rm -rf ./build/_output
RUN ./build/build.sh linux /bin

FROM registry.access.redhat.com/ubi8/ubi-minimal:8.2-267
COPY --from=build /bin/infinispan-operator /usr/local/bin/infinispan-operator
