#!/bin/sh
# Bundel lalu jalankan uji runtime (terjemahan + logika kecil di view).
# Hasil bundel ditaruh di dalam node_modules supaya resolusi dependency
# (zustand, react) tetap jalan.
set -e
cd "$(dirname "$0")/.."
npx vite build --ssr scripts/cek-runtime.ts --outDir node_modules/.cek-runtime --logLevel error
node node_modules/.cek-runtime/cek-runtime.js
