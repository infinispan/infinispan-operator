FROM alpine:latest
RUN apk add --no-cache -X http://dl-cdn.alpinelinux.org/alpine/edge/testing hub make git bash openssh-client
WORKDIR /infinispan-operator/
COPY . /infinispan-operator/
