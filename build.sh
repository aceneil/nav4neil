#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")"

test_mode=false
no_binary=false
debug=false
for arg in "$@"; do
  case "$arg" in
    --test) test_mode=true ;;
    --no-binary) no_binary=true ;;
    --debug) debug=true ;;
    -h|--help)
      echo "usage: $0 [--test] [--no-binary] [--debug]"
      echo "  --test       also run go test ./..."
      echo "  --no-binary  run gofmt, go vet, and tests without building"
      echo "  --debug      retain debug symbols in the binary"
      exit 0
      ;;
    *)
      echo "unknown option: $arg" >&2
      exit 2
      ;;
  esac
done

gofmt -w cmd internal
go vet ./...
if $test_mode || $no_binary; then
  go test ./...
fi
if $no_binary; then
  exit 0
fi

mkdir -p bin
ldflags=(-trimpath)
if ! $debug; then
  ldflags+=(-ldflags '-s -w')
fi
CGO_ENABLED=0 go build "${ldflags[@]}" -o bin/neilwz-nav-tui ./cmd/neilwz-nav-tui
printf 'built bin/neilwz-nav-tui\n'
