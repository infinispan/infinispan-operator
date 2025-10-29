#!/bin/bash

set -euo pipefail

YAML_FILE="$1"
NEW_VERSION="$2"
REPLACES_VERSION="$3"

# Ensure file exists
if [ ! -f "$YAML_FILE" ]; then
  echo "File not found: $YAML_FILE"
  exit 1
fi

# Check if NEW_VERSION already exists
if grep -q "name: ${NEW_VERSION}" "$YAML_FILE"; then
  echo "Version ${NEW_VERSION} already exists in $YAML_FILE. Aborting."
  exit 1
fi

# Check if REPLACES_VERSION exists
if ! grep -q "name: ${REPLACES_VERSION}" "$YAML_FILE"; then
  echo "Replaces version ${REPLACES_VERSION} not found in $YAML_FILE"
  exit 1
fi

INDENT=""
TMP_FILE=$(mktemp)
INSERTED="false"

while IFS= read -r line || [ -n "$line" ]; do
  if echo "$line" | grep -qE "^[[:space:]]*- name: ${REPLACES_VERSION}$"; then
    INDENT=$(echo "$line" | sed -E 's/^([[:space:]]*).*/\1/')

    echo "${INDENT}- name: ${NEW_VERSION}" >> "$TMP_FILE"
    echo "${INDENT}  replaces: ${REPLACES_VERSION}" >> "$TMP_FILE"
    INSERTED="true"
  fi

  echo "$line" >> "$TMP_FILE"
done < "$YAML_FILE"

if [ "$INSERTED" = "false" ]; then
  echo "Could not find where to insert ${NEW_VERSION}, prepending instead."
  {
    echo "- name: ${NEW_VERSION}"
    echo "  replaces: ${REPLACES_VERSION}"
    cat "$TMP_FILE"
  } > "${TMP_FILE}_new"
  mv "${TMP_FILE}_new" "$TMP_FILE"
fi

mv "$TMP_FILE" "$YAML_FILE"

echo "Successfully inserted ${NEW_VERSION} replacing ${REPLACES_VERSION}"
