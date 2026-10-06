#!/bin/sh
# Bundel lalu jalankan uji runtime (terjemahan, logika kecil di view, dan
# perilaku tumpukan lapisan Escape).
# Hasil bundel ditaruh di dalam node_modules supaya resolusi dependency
# (zustand, react) tetap jalan.
set -e
cd "$(dirname "$0")/.."

jalankan() { # $1 = nama berkas entry di scripts/
  nama="${1%.ts}" # vite menamai keluarannya .js, tanpa ekstensi sumbernya
  npx vite build --ssr "scripts/$1" --outDir "node_modules/.cek-$nama" --logLevel error
  node "node_modules/.cek-$nama/$nama.js"
}

jalankan cek-runtime.ts
jalankan cek-files-context.ts
jalankan cek-fail2ban.ts
jalankan cek-fail2ban-adopt.ts
jalankan cek-fail2ban-geoip.ts
jalankan cek-fail2ban-full.ts
