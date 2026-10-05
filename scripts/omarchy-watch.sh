#!/usr/bin/env bash
set -euo pipefail

repo_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
installer="$repo_dir/scripts/omarchy-install.sh"

if ! command -v inotifywait >/dev/null; then
  echo "omarchy-watch needs inotifywait (inotify-tools)." >&2
  exit 1
fi

"$installer"
echo "Watching Go source, go.mod, go.sum, and the SVG icon. Press Ctrl-C to stop."
while inotifywait -q -r -e close_write,create,delete,moved_to,moved_from \
  --include '(\.go$|\.svg$|/go\.(mod|sum)$)' "$repo_dir"; do
  if ! "$installer"; then
    echo "Build failed; keeping the previous installed binary." >&2
  fi
done
