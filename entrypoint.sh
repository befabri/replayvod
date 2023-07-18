#!/bin/sh
# entrypoint.sh

addgroup -g $PGID abc
adduser -u $PUID -G abc -D abc


# Change the ownership of the volume directories
chown -R abc:abc /app/log /app/public /app/data /app/bin
chmod +x bin/yt-dlp

# Execute the command (CMD [ "node", "app.js" ])
exec "$@"
