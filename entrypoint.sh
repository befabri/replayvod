#!/bin/sh
# entrypoint.sh

addgroup -g $PGID appgroup

adduser -D -u $PUID -s /bin/sh -G appgroup appuser

su - appuser

exec "$@"
