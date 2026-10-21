#!/bin/sh
set -eu

packet_repo=$(CDPATH= cd "$(dirname "$0")/.." && pwd)
cd "$packet_repo"

# Generated models/routes retain their generator's formatting.
set -- "${1:-check}" main.go main_test.go router.go biz/handler internal web
mode=$1
shift
case "$mode" in
  fix)
    gofmt -w "$@"
    ;;
  check)
    unformatted=$(gofmt -l "$@")
    if [ -n "$unformatted" ]; then
      printf 'Run make fmt to format:\n%s\n' "$unformatted" >&2
      exit 1
    fi
    ;;
  *)
    printf 'Usage: %s [check|fix]\n' "$0" >&2
    exit 2
    ;;
esac
