#!/usr/bin/env bash

set -Eeuo pipefail

format="${1:-svg}"

case "$format" in
  svg|png) ;;
  *)
    printf 'Unsupported format: %s. Use svg or png.\n' "$format" >&2
    exit 2
    ;;
esac

root_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
source_file="$root_dir/diagrams/c4-container.puml"
output_dir="$root_dir/diagrams/out"
output_file="$output_dir/c4-container.$format"

if ! command -v plantuml >/dev/null 2>&1; then
  printf 'PlantUML was not found. Run "make install" first.\n' >&2
  exit 1
fi

mkdir -p "$output_dir"
plantuml "-t${format}" -charset UTF-8 -failfast2 -o "$output_dir" "$source_file"

if [[ ! -f "$output_file" ]]; then
  printf 'PlantUML finished, but %s was not created.\n' "$output_file" >&2
  exit 1
fi

printf 'C4 diagram: %s\n' "$output_file"
