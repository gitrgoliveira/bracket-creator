#!/usr/bin/env bash

# Stamp version/commit/build-date into the .txt files consumed by //go:embed.
#
# Priority for each value:
#   1. An explicit environment variable (VERSION / GIT_COMMIT / BUILD_DATE).
#      Docker builds set these from --build-arg because .dockerignore excludes
#      .git, so the git probes below cannot work inside the build container.
#   2. Information derived from the local git checkout.
#   3. A sensible literal fallback so the binary never embeds an empty string
#      (an empty build date would break the version page and version_test.go).

echo -n "${GIT_COMMIT:-$(git rev-parse HEAD 2>/dev/null || echo "unknown")}" > commit.txt

echo -n "${VERSION:-$(git describe --tags --exact-match 2>/dev/null \
  || git symbolic-ref -q --short HEAD 2>/dev/null \
  || git rev-parse --short HEAD 2>/dev/null \
  || echo "dev")}" > version.txt

echo -n "${BUILD_DATE:-$(git show -s --format=%ci 2>/dev/null \
  || date -u +"%Y-%m-%d %H:%M:%S %z")}" > build_date.txt
