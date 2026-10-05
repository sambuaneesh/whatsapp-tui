#!/usr/bin/env bash
set -euo pipefail

repo_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
bin_dir="$HOME/.local/bin"
binary="$bin_dir/whatsapp-tui"
launcher="$HOME/.local/share/applications/WhatsApp TUI.desktop"
icon="$repo_dir/assets/whatsapp-tui.svg"
go_version=$(awk '$1 == "go" { print $2; exit }' "$repo_dir/go.mod")

if [[ -z $go_version ]]; then
  echo "Could not read the Go version from go.mod" >&2
  exit 1
fi

mkdir -p "$bin_dir"
build_file=$(mktemp "$bin_dir/.whatsapp-tui.XXXXXX")
trap 'rm -f -- "$build_file"' EXIT

(
  cd "$repo_dir"
  mise exec "go@$go_version" -- go build -o "$build_file" .
)
chmod 755 "$build_file"
mv -f -- "$build_file" "$binary"

omarchy tui install "WhatsApp TUI" "$binary" tile "$icon"
desktop-file-edit --set-key=Comment \
  --set-value="Vim-style WhatsApp client for the terminal" "$launcher"
desktop-file-validate "$launcher"
update-desktop-database "$HOME/.local/share/applications" 2>/dev/null || true
echo "Installed this checkout to $binary; open WhatsApp TUI from the app launcher."
