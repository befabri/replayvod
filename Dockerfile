FROM node:16-alpine AS base

ARG PUID=1000
ARG PGID=1000

ENV NODE_ENV=production \
    PORT=8080 \
    PUID=$PUID \
    PGID=$PGID

WORKDIR /app

RUN apk add --update python3 py3-pip && \
    python3 -m ensurepip && \
    pip3 install --upgrade pip setuptools && \
    addgroup -g $PGID appgroup && \
    adduser -D -u $PUID -G appgroup appuser

RUN python3 --version && pip3 --version

COPY package*.json ./

USER appuser

RUN npm install && npm cache clean --force

RUN npm ci --only=production

RUN apk add --update ffmpeg

COPY --chown=appuser:appgroup ./dist ./

VOLUME ["/app/log", "/app/public"]

EXPOSE $PORT

CMD [ "node", "app.js" ]
