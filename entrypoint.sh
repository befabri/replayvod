#!/bin/sh
# entrypoint.sh

APP_USER="appuser"
APP_GROUP="appgroup"

# Check if the group exists and create it if it doesn't
if ! getent group $PGID > /dev/null; then
    addgroup -g $PGID $APP_GROUP
else
    APP_GROUP=$(getent group $PGID | cut -d: -f1)
fi

# Check if the user exists and create it if it doesn't
if ! getent passwd $PUID > /dev/null; then
    adduser -D -u $PUID -s /bin/sh -G $APP_GROUP $APP_USER
else
    APP_USER=$(getent passwd $PUID | cut -d: -f1)
    addgroup $APP_USER $APP_GROUP
fi

# Change the ownership of the volume directories
chown -R $PUID:$PGID /app

# Switch to the new user
su - $APP_USER

# Execute the command (CMD [ "node", "app.js" ])
exec "$@"
