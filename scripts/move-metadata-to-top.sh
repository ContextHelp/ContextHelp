#!/bin/bash
# Move all metadata from bottom to top of documentation files

set -e

DOCS_DIR="/Users/jadb/.w/ideacrafterslabs/ctxt/docs"

# Find all markdown files with metadata at the bottom
grep -rl "^\*\*Last Updated\*\*:\|^\*\*Version\*\*:\|^\*\*Status\*\*:\|^\*\*Maintained by\*\*:" "$DOCS_DIR" --include="*.md" | while read -r file; do

  # Check if metadata exists beyond line 10
  if tail -n +10 "$file" | grep -q "^\*\*Last Updated\*\*:\|^\*\*Version\*\*:\|^\*\*Status\*\*:\|^\*\*Maintained by\*\*:"; then

    echo "Processing: $file"

    # Extract metadata block from bottom (find last occurrence)
    metadata=$(grep -n "^\*\*Last Updated\*\*:" "$file" | tail -1 | cut -d: -f1)

    if [ -n "$metadata" ]; then
      # Get the heading (first line)
      heading=$(head -n 1 "$file")

      # Get current Version line (should be at line 3)
      version_line=$(sed -n '3p' "$file")

      # Extract metadata lines starting from the found line
      bottom_metadata=$(tail -n +"$metadata" "$file" | grep "^\*\*")

      # Get content before metadata at bottom (remove last few lines)
      end_line=$((metadata - 1))

      # Remove the old metadata block at bottom and the version 0.1.0 we added
      content=$(sed -n "4,${end_line}p" "$file")

      # Reconstruct file: heading + metadata (updated) + content
      {
        echo "$heading"
        echo ""

        # Use 0.1.0 for version, keep other metadata from bottom
        echo "**Version:** 0.1.0"
        echo "$bottom_metadata" | grep -v "^\*\*Version\*\*:"
        echo ""
        echo "$content"
      } > "${file}.tmp"

      mv "${file}.tmp" "$file"
      echo "✓ Moved metadata to top: $file"
    fi
  fi
done

echo ""
echo "Done! All metadata moved to top of files."
