#!/usr/bin/env bash
#
# Build the workflow-dispatch fork's server image and push it to the
# self-hosted registry. Builds the UI + CGO server binary inside the image
# (see docker/Dockerfile.custom), so it runs regardless of the host OS.
#
# Usage:
#   ./build-and-push.sh                 # build + push doc.msdci.nl/woodpecker-server:custom (+ :<gitsha>)
#   PUSH=false ./build-and-push.sh      # build locally only, do not push
#   PLATFORM=linux/arm64 ./build-and-push.sh
#   TAG=v3.20.1-fork.1 ./build-and-push.sh
#
# TAG defaults to "custom": VERSION is then git describe (without the "v"),
# e.g. 3.20.1-67-g555225f, so the UI shows which fork commit is running.
#
# Any other TAG is a release: the image is tagged <REGISTRY>/<IMAGE>:<TAG>,
# built with VERSION=<TAG without "v">, and after a successful push HEAD is
# git-tagged <TAG> and that tag is pushed to origin. Requires a clean tree
# and a tag name that does not exist yet. Update checks coerce
# 3.20.1-fork.1 to 3.20.1, so no false "update available".
# Override any of: REGISTRY, IMAGE, TAG, PLATFORM, VERSION, PUSH
set -euo pipefail

cd "$(dirname "$0")"

REGISTRY="${REGISTRY:-doc.msdci.nl}"
IMAGE="${IMAGE:-woodpecker-server}"
TAG="${TAG:-custom}"
PLATFORM="${PLATFORM:-linux/amd64}"
PUSH="${PUSH:-true}"

GIT_SHA="$(git rev-parse --short HEAD 2>/dev/null || echo unknown)"
GIT_DESCRIBE="$(git describe --tags 2>/dev/null | sed 's/^v//' || true)"

if [ "${TAG}" != "custom" ]; then
  if [ -n "$(git status --porcelain)" ]; then
    echo "!! working tree not clean, commit or stash before tagging ${TAG}" >&2
    exit 1
  fi
  if git rev-parse -q --verify "refs/tags/${TAG}" >/dev/null; then
    echo "!! git tag ${TAG} already exists" >&2
    exit 1
  fi
  VERSION="${VERSION:-${TAG#v}}"
fi

VERSION="${VERSION:-${GIT_DESCRIBE:-${TAG}-${GIT_SHA}}}"

REF="${REGISTRY}/${IMAGE}:${TAG}"
REF_SHA="${REGISTRY}/${IMAGE}:${GIT_SHA}"

echo ">> building ${REF}"
echo "   platform: ${PLATFORM}   version: ${VERSION}   push: ${PUSH}"

# buildx is required for --platform; create a builder once if missing
if ! docker buildx inspect wp-builder >/dev/null 2>&1; then
  docker buildx create --name wp-builder --use >/dev/null
else
  docker buildx use wp-builder
fi

OUTPUT_ARGS=()
if [ "${PUSH}" = "true" ]; then
  OUTPUT_ARGS+=(--push)
else
  # single-arch local load (cannot --load a multi-arch manifest)
  OUTPUT_ARGS+=(--load)
fi

docker buildx build \
  --platform "${PLATFORM}" \
  -f docker/Dockerfile.custom \
  --build-arg "VERSION=${VERSION}" \
  -t "${REF}" \
  -t "${REF_SHA}" \
  "${OUTPUT_ARGS[@]}" \
  .

if [ "${TAG}" != "custom" ]; then
  git tag -a "${TAG}" -m "Fork release ${TAG}"
  if [ "${PUSH}" = "true" ]; then
    git push origin "refs/tags/${TAG}"
  fi
fi

echo
echo ">> done"
echo "   ${REF}"
echo "   ${REF_SHA}"
[ "${TAG}" != "custom" ] && echo "   git tag ${TAG} pushed to origin"
if [ "${PUSH}" = "true" ]; then
  echo
  echo "   Next: point your compose at this tag and redeploy, e.g."
  echo "     woodpecker:       image: ${REF}"
  echo "     # then: sh ~/up-services.sh woodpecker"
  echo "   (the agent image is unchanged — these changes are server + UI only)"
fi
