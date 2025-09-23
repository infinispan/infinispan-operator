#!/bin/bash
set -e

# Fetch all tags
git fetch --tags

# Get the latest tag (sorted by creation date)
LATEST_TAG=$(git tag --sort=-creatordate | head -n 1)
echo "Latest tag: $LATEST_TAG"

# Remove  .Final suffix if present
BASE_VERSION=${LATEST_TAG%.Final}

echo "Previous version: $BASE_VERSION"

echo "prev_ver=$BASE_VERSION" >> "$GITHUB_OUTPUT"

IFS='.' read -r MAJOR MINOR PATCH <<< "$BASE_VERSION"

# Increment PATCH
PATCH=$((PATCH + 1))

# Build new version and tag
NEW_TAG="$MAJOR.$MINOR.$PATCH"

echo "New version: $NEW_TAG"

echo "new_tag=$NEW_TAG" >> "$GITHUB_OUTPUT"

# Set Next version
IFS='.' read -r MAJOR MINOR PATCH <<< "${NEW_TAG#v}"
PATCH="${PATCH%%[^0-9]*}" 
PATCH=$((PATCH + 1))
NEXT_VERSION="$MAJOR.$MINOR.$PATCH"

echo "Next version: $NEXT_VERSION"

echo "next_version=$NEXT_VERSION" >> "$GITHUB_OUTPUT"
