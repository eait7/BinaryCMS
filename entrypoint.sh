#!/bin/sh
# BinaryCMS startup wrapper
# Checks for a staged update in data/gocms_server_next and applies it before starting.

BINARY="/app/gocms_server"
STAGED="/app/data/gocms_server_next"

if [ -f "$STAGED" ]; then
    echo "🔄 Applying staged core update..."
    cp "$STAGED" "$BINARY" && chmod 755 "$BINARY" && rm -f "$STAGED"
    if [ $? -eq 0 ]; then
        echo "✅ Core binary updated successfully."
    else
        echo "⚠️  Failed to apply staged update, starting with existing binary."
    fi
fi

exec gosu gocms "$BINARY" "$@"
