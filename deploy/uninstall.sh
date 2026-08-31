#!/bin/bash
# Pembungkus: isi skripnya ada di internal/helper/uninstall.sh supaya bisa
# ditanam ke binary helper (go:embed) — dengan begitu panel bisa meng-uninstall
# dirinya sendiri di mesin yang tidak punya checkout sumber, dan hanya ada SATU
# salinan langkah uninstall yang perlu dijaga tetap benar.
exec bash "$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/internal/helper/uninstall.sh" "$@"
