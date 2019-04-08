FROM golang:1.11.5 AS build
RUN go get -u github.com/golang/dep/cmd/dep
COPY Gopkg.lock Gopkg.toml /go/src/github.com/infinispan/infinispan-operator/
WORKDIR /go/src/github.com/infinispan/infinispan-operator/
RUN dep ensure -vendor-only
COPY . /go/src/github.com/infinispan/infinispan-operator/
RUN rm -rf build/_output
RUN ./build/build.sh linux /bin

FROM scratch
COPY --from=build /bin/infinispan-operator /usr/local/bin/infinispan-operator
