# Base Image
FROM node:16-alpine AS base

ENV NODE_ENV=production \
    PORT=8080 

WORKDIR /app

RUN apk add --update python3 py3-pip && \
    python3 -m ensurepip && \
    pip3 install --upgrade pip setuptools

RUN python3 --version && pip3 --version

COPY package*.json ./

RUN npm install && npm cache clean --force

RUN npm ci --only=production

RUN apk add --update ffmpeg

COPY ./dist ./
COPY ./entrypoint.sh /entrypoint.sh

RUN chmod +x /entrypoint.sh

VOLUME ["/app/log", "/app/public"]

EXPOSE $PORT

ENTRYPOINT ["/entrypoint.sh"]

CMD [ "node", "app.js" ]
