#!/bin/sh
# Trusted local jobs only: the Docker socket provides host-equivalent control.
set -eu
test -n "${WORKSPACE:-}"
test -n "${HOSTNAME:-}"
docker build -f Dockerfile.ci -t buildlens-ci:local .
state=$(mktemp -d)
cleanup() {
  if [ -s "$state/container-id" ]; then
    container=$(cat "$state/container-id")
    case "$container" in *[!0-9a-f]*|'') ;; *) docker rm -f "$container" >/dev/null 2>&1 || true ;; esac
  fi
  rm -f "$state/container-id"
  rmdir "$state"
}
trap cleanup EXIT HUP INT TERM
docker run --rm --memory 1536m --cidfile "$state/container-id" \
  --volumes-from "$HOSTNAME" \
  -v buildlens-gomod:/go/pkg/mod -v buildlens-gocache:/root/.cache/go-build \
  -w "$WORKSPACE" \
  -e TESTCONTAINERS_HOST_OVERRIDE=host.docker.internal \
  -e TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE=/var/run/docker.sock \
  buildlens-ci:local sh tools/ci-inner.sh
