#!/bin/sh
# entrypoint.sh

# Check if the group exists and create it if it doesn't
if ! getent group $PGID > /dev/null; then
    addgroup -g $PGID appgroup
fi

# Check if the user exists and create it if it doesn't
if ! getent passwd $PUID > /dev/null; then
    adduser -D -u $PUID -s /bin/sh -G appgroup appuser
else
    addgroup $(getent passwd $PUID | cut -d: -f1) appgroup
fi

# Change the ownership of the volume directories
chown -R $PUID:$PGID /app/log /app/public

# Switch to the new user
su - $(getent passwd $PUID | cut -d: -f1)

# Execute the command (CMD [ "node", "app.js" ])
exec "$@"
