#!/bin/bash

# Release script for OpenGate
# Increments patch version, creates a git tag, and pushes to origin.

set -e

# Fetch latest tags
git fetch --tags

# Get current version
LATEST_TAG=$(git tag -l "v*" | sort -V | tail -n1)

if [ -z "$LATEST_TAG" ]; then
    NEW_TAG="v0.1.1"
else
    # Split version into components
    VERSION=${LATEST_TAG#v}
    MAJOR=$(echo $VERSION | cut -d. -f1)
    MINOR=$(echo $VERSION | cut -d. -f2)
    PATCH=$(echo $VERSION | cut -d. -f3)
    
    # Increment patch
    NEW_PATCH=$((PATCH + 1))
    NEW_TAG="v$MAJOR.$MINOR.$NEW_PATCH"
fi

echo "Current version: $LATEST_TAG"
echo "New version:     $NEW_TAG"

# Create tag
git tag -a "$NEW_TAG" -m "Release $NEW_TAG"

# Push tag
git push origin "$NEW_TAG"

echo "Release $NEW_TAG created and pushed successfully."
