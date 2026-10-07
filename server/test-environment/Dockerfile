FROM golang:1.26.8-alpine3.23@sha256:a8fa79c5bd40d880b52bd3b6d7669ecdcfd00e85facdd427d279efb5ddd79cb1
RUN apk add --no-cache build-base git docker-cli
ENV GOTOOLCHAIN=local GOPATH=/workspace/.offtest-home/go
