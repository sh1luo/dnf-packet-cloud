#!/bin/sh
set -eu

packet_repo=$(CDPATH= cd "$(dirname "$0")/.." && pwd)
cd "$packet_repo"

export STORAGE_MEDIA=lfs
export PACKETS_FILE_PATH="${PACKETS_FILE_PATH:-.runtime/packets}"
export PACKET_HOST_PORTS="${PACKET_HOST_PORTS:-127.0.0.1:8080}"
mkdir -p "$(dirname "$PACKETS_FILE_PATH")"

case "${1:-debug}" in
  debug) ;;
  demo)
    # Never replace an existing local dataset.
    if [ ! -e "$PACKETS_FILE_PATH" ] && [ ! -L "$PACKETS_FILE_PATH" ]; then
      cp -n "$packet_repo/examples/packets.json" "$PACKETS_FILE_PATH"
    fi
    ;;
  *)
    printf 'Usage: %s [debug|demo]\n' "$0" >&2
    exit 2
    ;;
esac

printf 'Local data: %s\nManagement: http://%s/v1/packet/edit\n' "$PACKETS_FILE_PATH" "$PACKET_HOST_PORTS"
exec go run .
