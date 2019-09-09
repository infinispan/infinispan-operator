FROM alpine:latest
RUN apk add --no-cache -X http://dl-cdn.alpinelinux.org/alpine/edge/testing hub make git bash openssh-client
WORKDIR /go/src/github.com/infinispan/infinispan-operator/
COPY . /go/src/github.com/infinispan/infinispan-operator/
