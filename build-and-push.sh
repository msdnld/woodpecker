#!/usr/bin/env bash
#
# Build the workflow-dispatch fork's server image and push it to the
# self-hosted registry. Builds the UI + CGO server binary inside the image
# (see docker/Dockerfile.custom), so it runs regardless of the host OS.
#
# Usage:
#   ./build-and-push.sh                 # build + push doc.msdci.nl/woodpecker-server:custom (+ :<gitsha>)
#   PUSH=false ./build-and-push.sh      # build locally only, do not push
#   TAG=v3-dispatch ./build-and-push.sh # custom tag
#   PLATFORM=linux/arm64 ./build-and-push.sh
#
# Override any of: REGISTRY, IMAGE, TAG, PLATFORM, VERSION, PUSH
set -euo pipefail

cd "$(dirname "$0")"

REGISTRY="${REGISTRY:-doc.msdci.nl}"
IMAGE="${IMAGE:-woodpecker-server}"
TAG="${TAG:-custom}"
PLATFORM="${PLATFORM:-linux/amd64}"
PUSH="${PUSH:-true}"

GIT_SHA="$(git rev-parse --short HEAD 2>/dev/null || echo unknown)"
VERSION="${VERSION:-${TAG}-${GIT_SHA}}"

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

echo
echo ">> done"
echo "   ${REF}"
echo "   ${REF_SHA}"
if [ "${PUSH}" = "true" ]; then
  echo
  echo "   Next: point your compose at this tag and redeploy, e.g."
  echo "     woodpecker:       image: ${REF}"
  echo "     # then: sh ~/up-services.sh woodpecker"
  echo "   (the agent image is unchanged — these changes are server + UI only)"
fi
