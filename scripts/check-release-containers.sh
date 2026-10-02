#!/bin/sh
# SPDX-License-Identifier: Apache-2.0
# Exercise the release Dockerfile, including the target platform's system tools.
set -eu
cd "$(dirname "$0")/.."
acor_context=$(mktemp -d)
acor_image_prefix="acor-release-check-$$"
cleanup() {
  rm -rf "$acor_context"
  for acor_arch in amd64 arm64; do
    docker image rm "$acor_image_prefix:$acor_arch" >/dev/null 2>&1 || true
  done
}
trap cleanup EXIT HUP INT TERM
cp LICENSE NOTICE "$acor_context/"
for acor_arch in ${ACOR_RELEASE_ARCHES:-amd64 arm64}; do
  case "$acor_arch" in
    amd64) acor_machine=x86_64 ;;
    arm64) acor_machine=aarch64 ;;
    *) echo "unsupported release architecture: $acor_arch" >&2; exit 2 ;;
  esac
  mkdir -p "$acor_context/linux/$acor_arch"
  CGO_ENABLED=0 GOOS=linux GOARCH="$acor_arch" go build \
    -ldflags='-X main.version=release-check' \
    -o "$acor_context/linux/$acor_arch/acor" ./cmd/acor
  docker build --platform="linux/$acor_arch" \
    -f "${ACOR_RELEASE_DOCKERFILE:-Dockerfile.goreleaser}" \
    -t "$acor_image_prefix:$acor_arch" "$acor_context"
  acor_version=$(docker run --rm --platform="linux/$acor_arch" "$acor_image_prefix:$acor_arch" version)
  test "$acor_version" = release-check
  docker run --rm --platform="linux/$acor_arch" --entrypoint /bin/sh \
    "$acor_image_prefix:$acor_arch" -c \
    'echo "system architecture: $(uname -m), expected: $1"; test "$(uname -m)" = "$1" && test -s /usr/share/doc/acor/LICENSE && test -s /usr/share/doc/acor/NOTICE' \
    sh "$acor_machine"
  echo "release container passed: linux/$acor_arch"
done
