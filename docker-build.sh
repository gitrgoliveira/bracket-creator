#!/bin/bash
set -euo pipefail
GO_VERSION="${GO_VERSION:-1.26.2}"
GIT_COMMIT="${GIT_COMMIT:-$(git rev-parse --short HEAD)}"
VERSION="${VERSION:-$(git describe --tags --exact-match 2>/dev/null || printf '%s' "$GIT_COMMIT")}"

docker build \
	--build-arg GO_VERSION="$GO_VERSION" \
	--build-arg VERSION="$VERSION" \
	--build-arg GIT_COMMIT="$GIT_COMMIT" \
	-t ghcr.io/gitrgoliveira/bracket-creator:latest \
	.
grype ghcr.io/gitrgoliveira/bracket-creator:latest --fail-on medium
