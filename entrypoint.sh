#!/bin/sh
# entrypoint.sh

# Change the ownership of the volume directories
chown -R $PUID:$PGID /app/log /app/public /app/data /app/bin
chmod +x bin/yt-dlp

# Execute the command (CMD [ "node", "app.js" ])
exec "$@"
