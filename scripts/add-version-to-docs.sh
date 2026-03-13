#!/bin/bash
# Add version 0.1.0 to all markdown documentation files

set -e

VERSION="0.1.0"
DOCS_DIR="/Users/jadb/.w/ideacrafterslabs/ctxt/docs"

# Find all markdown files
find "$DOCS_DIR" -type f -name "*.md" | while read -r file; do
  # Check if file already has a version
  if grep -q "^\*\*Version:\*\*" "$file"; then
    echo "Skipping $file (already has version)"
    continue
  fi

  # Check if file starts with a heading
  if head -n 1 "$file" | grep -q "^#"; then
    # Get the first line (heading)
    heading=$(head -n 1 "$file")

    # Get the rest of the file
    rest=$(tail -n +2 "$file")

    # Create new content with version after heading
    {
      echo "$heading"
      echo ""
      echo "**Version:** $VERSION"
      echo "$rest"
    } > "${file}.tmp"

    mv "${file}.tmp" "$file"
    echo "✓ Added version to: $file"
  else
    # File doesn't start with heading, add version at top
    {
      echo "**Version:** $VERSION"
      echo ""
      cat "$file"
    } > "${file}.tmp"

    mv "${file}.tmp" "$file"
    echo "✓ Added version to: $file"
  fi
done

echo ""
echo "Done! Added version $VERSION to all documentation files."
