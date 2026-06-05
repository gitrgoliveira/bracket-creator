#!/usr/bin/env bash

echo -n $(git rev-parse HEAD 2>/dev/null || echo "unknown") > commit.txt

echo -n $(git describe --tags --exact-match 2>/dev/null \
  || git symbolic-ref -q --short HEAD 2>/dev/null \
  || git rev-parse --short HEAD 2>/dev/null \
  || echo "dev") &> version.txt

echo -n $(git show -z -s --format=%ci 2>/dev/null || echo "") > build_date.txt
