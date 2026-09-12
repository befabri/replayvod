FROM golang:1.26.0-alpine3.23@sha256:d4c4845f5d60c6a974c6000ce58ae079328d03ab7f721a0734277e69905473e5
RUN apk add --no-cache build-base git docker-cli
ENV GOTOOLCHAIN=local GOPATH=/workspace/.offtest-home/go
