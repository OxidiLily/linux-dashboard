#!/usr/bin/env bash
set -euo pipefail
# Run as the account owner. All destination I/O stays descriptor-relative.
[[ $# -ge 1 && $# -le 2 ]] || { printf 'Usage: %s <home> [seed-root]\n' "$0" >&2; exit 2; }
script_dir="$(cd -- "$(dirname -- "$0")" && pwd)"
seed_root="${2:-$(cd -- "$script_dir/.." && pwd)}"
exec /usr/bin/python3 -I -B "$script_dir/install-ai-state.py" "$1" "$seed_root"
