#!/usr/bin/env bash
# Install Cloudflare's security-audit skill into the shared AI Skills directory.
set -euo pipefail

[[ $# -eq 1 ]] || { printf 'Usage: %s <AI setup root>\n' "$0" >&2; exit 2; }
ai_root="$1"
skills_dir="$ai_root/Skills"
target="$skills_dir/security-audit"
repo=https://github.com/cloudflare/security-audit-skill.git

mkdir -p "$skills_dir"
if [[ -e "$target" || -L "$target" ]]; then
  [[ -f "$target/SKILL.md" && ! -L "$target" ]] || {
    printf 'Refusing unsafe/incomplete skill target: %s\n' "$target" >&2; exit 1;
  }
  printf 'Security audit skill already installed: %s\n' "$target"
  exit 0
fi

tmp="$(mktemp -d "$skills_dir/.security-audit.XXXXXX")"
trap 'rm -rf -- "$tmp"' EXIT
git clone --quiet --depth 1 --filter=blob:none --sparse "$repo" "$tmp/repo"
git -C "$tmp/repo" sparse-checkout set skills/security-audit
[[ -s "$tmp/repo/skills/security-audit/SKILL.md" ]] || {
  printf 'Upstream skill is missing SKILL.md\n' >&2; exit 1;
}
# Publish whole upstream skill tree atomically; preserve any existing installation.
mv -- "$tmp/repo/skills/security-audit" "$target"
printf 'Installed Cloudflare security-audit skill: %s\n' "$target"
