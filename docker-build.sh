#!/bin/bash
# test image locally using grype since Docker rate limited make docker/build earlier
docker build \
	--build-arg GO_VERSION=1.26.1 \
	--build-arg VERSION=6d4cafb \
	--build-arg GIT_COMMIT=6d4cafb \
	-t ghcr.io/gitrgoliveira/bracket-creator:latest \
	.
grype ghcr.io/gitrgoliveira/bracket-creator:latest --fail-on medium
