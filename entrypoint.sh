#!/bin/sh

if [ -n "$CONFIG_URL" ]; then
    echo "[INFO] Downloading config from remote URL"
    if curl -fsSLo /app/config.toml "$CONFIG_URL"; then
        echo "[INFO] Configuration downloaded successfully"
    else
        echo "[ERROR] Failed to download config from remote URL"
        exit 1
    fi
    if [ -n "$CONFIG_SHA256" ]; then
        config_sum="$(sha256sum /app/config.toml | awk '{print $1}')"
        if [ "$config_sum" != "$CONFIG_SHA256" ]; then
            echo "[ERROR] Remote config checksum mismatch"
            exit 1
        fi
        echo "[INFO] Remote config checksum verified"
    fi
fi

if [ ! -f /app/config.toml ]; then
    echo "[ERROR] Missing config.toml: Please provide the configuration file via mounting or CONFIG_URL"
    exit 1
fi
    
exec /app/saveany-bot
