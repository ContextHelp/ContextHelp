#!/bin/bash
# Clean up markdown files: remove trailing empty lines and trailing separators

set -e

DOCS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/docs"

echo "Cleaning up markdown file endings..."
echo ""

# Find all markdown files
find "$DOCS_DIR" -type f -name "*.md" | while read -r file; do
  # Check if file has trailing issues
  needs_cleanup=false

  # Check for trailing empty lines or trailing separator
  if tail -n 1 "$file" | grep -qE "^[[:space:]]*$|^---$"; then
    needs_cleanup=true
  fi

  if [ "$needs_cleanup" = true ]; then
    # Use Python to clean the file properly
    python3 -c "
import sys

with open('$file', 'r') as f:
    lines = f.readlines()

# Remove trailing empty lines
while lines and lines[-1].strip() == '':
    lines.pop()

# Remove trailing separator if it's the last line
if lines and lines[-1].strip() == '---':
    lines.pop()
    # Remove any empty lines before the separator
    while lines and lines[-1].strip() == '':
        lines.pop()

# Ensure file ends with single newline
if lines and not lines[-1].endswith('\n'):
    lines[-1] += '\n'

with open('$file', 'w') as f:
    f.writelines(lines)
"
    echo "✓ Cleaned: $file"
  fi
done

echo ""
echo "Done! All markdown files cleaned."
