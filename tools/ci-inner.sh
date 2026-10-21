#!/bin/sh
set -eu
test "$(go env GOVERSION)" = go1.27.1
test "$(golangci-lint version --short)" = 2.14.0
unformatted=$(gofmt -l cmd internal migrations)
if [ -n "$unformatted" ]; then printf '%s\n' "$unformatted"; exit 1; fi
echo 'gofmt: clean'
go vet ./...
echo 'go vet: PASS'
golangci-lint run ./...
go test -race -count=1 -p 1 ./...
