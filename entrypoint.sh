#!/bin/sh
# BinaryCMS startup wrapper
# Runs as root first to fix volume-mount ownership, then drops to gocms.

# Fix ownership of volume-mounted directories that may have been created by root
# on the host. This ensures gocms can read/write plugin databases, uploads, etc.
echo "Fixing volume ownership..."
chown -R gocms:gocms /app/plugins_data /app/data /app/uploads /app/plugins 2>/dev/null || true
chmod -R u+rwX /app/plugins_data /app/data /app/uploads /app/plugins 2>/dev/null || true
echo "Volume ownership fixed."

BINARY="/app/gocms_server"
STAGED="/app/data/gocms_server_next"

if [ -f "$STAGED" ]; then
    echo "Applying staged core update..."
    cp "$STAGED" "$BINARY" && chmod 755 "$BINARY" && chown gocms:gocms "$BINARY" && rm -f "$STAGED"
    if [ $? -eq 0 ]; then
        echo "Core binary updated successfully."
    else
        echo "Failed to apply staged update, starting with existing binary."
    fi
fi

# Drop privileges and run as gocms
exec gosu gocms "$BINARY" "$@"
