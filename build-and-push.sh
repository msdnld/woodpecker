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
#   RELEASE=true ./build-and-push.sh    # also cut a fork release tag (see below)
#   RELEASE=v3.20.1-fork.3 ./build-and-push.sh  # release with an explicit tag name
#
# VERSION defaults to git describe (without the "v"), e.g. 3.20.1-67-g555225f,
# so the UI shows which fork commit is running. Update checks coerce it to 3.20.1.
#
# RELEASE=true tags HEAD as v<upstream>-fork.<n>, where <upstream> is the
# nearest upstream release tag and <n> the next free number (v3.20.1-fork.1,
# v3.20.1-fork.2, ...). The image is built with that version, also tagged
# <REGISTRY>/<IMAGE>:<release> and, once the push succeeded, the git tag is
# pushed to origin. Requires a clean working tree.
# Override any of: REGISTRY, IMAGE, TAG, PLATFORM, VERSION, PUSH, RELEASE
set -euo pipefail

cd "$(dirname "$0")"

REGISTRY="${REGISTRY:-doc.msdci.nl}"
IMAGE="${IMAGE:-woodpecker-server}"
TAG="${TAG:-custom}"
PLATFORM="${PLATFORM:-linux/amd64}"
PUSH="${PUSH:-true}"

RELEASE="${RELEASE:-false}"

GIT_SHA="$(git rev-parse --short HEAD 2>/dev/null || echo unknown)"
GIT_TAG="$(git describe --tags 2>/dev/null | sed 's/^v//' || true)"

RELEASE_TAG=""
if [ "${RELEASE}" != "false" ]; then
  if [ -n "$(git status --porcelain)" ]; then
    echo "!! working tree not clean, commit or stash before cutting a release" >&2
    exit 1
  fi
  if [ "${RELEASE}" = "true" ]; then
    UPSTREAM_TAG="$(git describe --tags --abbrev=0 --exclude='*-fork.*' 2>/dev/null || true)"
    if [ -z "${UPSTREAM_TAG}" ]; then
      echo "!! no upstream release tag reachable from HEAD" >&2
      exit 1
    fi
    N=1
    while git rev-parse -q --verify "refs/tags/${UPSTREAM_TAG}-fork.${N}" >/dev/null; do
      N=$((N + 1))
    done
    RELEASE_TAG="${UPSTREAM_TAG}-fork.${N}"
  else
    RELEASE_TAG="${RELEASE}"
    if git rev-parse -q --verify "refs/tags/${RELEASE_TAG}" >/dev/null; then
      echo "!! tag ${RELEASE_TAG} already exists" >&2
      exit 1
    fi
  fi
  VERSION="${VERSION:-${RELEASE_TAG#v}}"
fi

VERSION="${VERSION:-${GIT_TAG:-${TAG}-${GIT_SHA}}}"

REF="${REGISTRY}/${IMAGE}:${TAG}"
REF_SHA="${REGISTRY}/${IMAGE}:${GIT_SHA}"
TAG_ARGS=(-t "${REF}" -t "${REF_SHA}")
if [ -n "${RELEASE_TAG}" ]; then
  REF_RELEASE="${REGISTRY}/${IMAGE}:${RELEASE_TAG#v}"
  TAG_ARGS+=(-t "${REF_RELEASE}")
fi

echo ">> building ${REF}"
echo "   platform: ${PLATFORM}   version: ${VERSION}   push: ${PUSH}"
[ -n "${RELEASE_TAG}" ] && echo "   release: ${RELEASE_TAG}"

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
  "${TAG_ARGS[@]}" \
  "${OUTPUT_ARGS[@]}" \
  .

if [ -n "${RELEASE_TAG}" ]; then
  git tag -a "${RELEASE_TAG}" -m "Fork release ${RELEASE_TAG}"
  if [ "${PUSH}" = "true" ]; then
    git push origin "refs/tags/${RELEASE_TAG}"
  fi
fi

echo
echo ">> done"
echo "   ${REF}"
echo "   ${REF_SHA}"
[ -n "${RELEASE_TAG}" ] && echo "   ${REF_RELEASE}   (git tag ${RELEASE_TAG})"
if [ "${PUSH}" = "true" ]; then
  echo
  echo "   Next: point your compose at this tag and redeploy, e.g."
  echo "     woodpecker:       image: ${REF}"
  echo "     # then: sh ~/up-services.sh woodpecker"
  echo "   (the agent image is unchanged — these changes are server + UI only)"
fi
