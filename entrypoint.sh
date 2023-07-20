#!/bin/sh
# entrypoint.sh

# Update user 'node' to have the specified UID and GID
if getent passwd $PUID > /dev/null; then
    usermod -u $PUID node
fi
if getent group $PGID > /dev/null; then
    groupmod -g $PGID node
fi

# Change the ownership of the volume directories
chown -R $PUID:$PGID /app/log /app/public /app/data /app/bin
chmod +x bin/yt-dlp

# Execute the command (CMD [ "node", "app.js" ])
exec "$@"
