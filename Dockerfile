FROM node:18-alpine AS base

ENV NODE_ENV=production \
    PORT=8080

WORKDIR /app

COPY package*.json ./

RUN npm install && npm cache clean --force

RUN npm ci --only=production

# Bundle app source
COPY ./dist ./
VOLUME ["/app/log", "/app/public"]
EXPOSE $PORT
CMD [ "node", "app.js" ]