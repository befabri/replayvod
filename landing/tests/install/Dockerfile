FROM alpine:3.21

RUN apk add --no-cache git openssl python3 shellcheck

WORKDIR /workspace

CMD ["sh", "/workspace/landing/tests/install/run.sh"]
