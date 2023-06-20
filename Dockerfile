FROM node:18-alpine AS base

ENV NODE_ENV=production \
    PORT=8080

WORKDIR /app

# Installing Python
RUN apk add --update python3 py3-pip && \
    python3 -m ensurepip && \
    pip3 install --upgrade pip setuptools

# Check Python & pip versions
RUN python3 --version && pip3 --version

COPY package*.json ./

RUN npm install && npm cache clean --force

RUN npm ci --only=production

# Bundle app source
COPY ./dist ./
VOLUME ["/app/log", "/app/public"]
EXPOSE $PORT
CMD [ "node", "app.js" ]
