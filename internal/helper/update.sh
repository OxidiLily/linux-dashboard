#!/bin/bash
# Skrip pembaruan panel — ditanam di binary helper (go:embed) dan ditulis ke
# /var/lib/linux-dashboard/update.sh saat tombol Update ditekan.
#
# Ditanam, bukan dibaca dari checkout, karena mesin yang belum pernah punya
# sumber di /usr/local/src pun harus bisa memulai pembaruan pertamanya.
#
# Yang dikerjakan cuma menyegarkan sumber lalu menyerahkan sisanya ke
# deploy/install.sh versi BARU — langkah pasang service, user sistem, dan
# restart sudah ada di sana, jadi perubahan cara pasang ikut terbawa update
# tanpa perlu menyalin langkahnya ke sini.
set -euo pipefail

REPO="${REPO:-https://github.com/OxidiLily/linux-dashboard.git}"
BRANCH="${BRANCH:-main}"
SRC="${SRC:-/usr/local/src/go-react-linux-dashboard}"

# Ujung branch dibaca lebih dulu, dan itulah satu-satunya sasaran: pembaruan
# baru dianggap berhasil kalau sumber berakhir persis di commit ini.
tip="$(git ls-remote "$REPO" "refs/heads/${BRANCH}" | cut -f1)"

if [[ -d "$SRC/.git" ]]; then
  echo "[i] Menyegarkan sumber di ${SRC} (branch ${BRANCH})…"
  # reset ke FETCH_HEAD, BUKAN ke origin/<branch>: remote-tracking ref hanya
  # ikut terbarui kalau refspec fetch checkout ini memang memetakannya, dan
  # checkout yang dibuat dengan cara lain akan selamanya reset ke ref lama
  # yang sama — pembaruan terlihat sukses tapi versinya tidak pernah pindah.
  # FETCH_HEAD selalu berisi apa yang baru saja diambil.
  git -C "$SRC" fetch --depth 1 --force origin "$BRANCH" &&
    git -C "$SRC" reset --hard FETCH_HEAD || true
fi

# Apa pun yang membuat penyegaran di atas gagal — ref lama, berkas shallow
# rusak, fetch putus, sumber belum ada — dijawab dengan cara yang sama:
# ambil ulang dari nol. Lebih lambat, tapi tidak ada keadaan yang membuat
# panel diam-diam terus memasang versi lama.
lokal="$(git -C "$SRC" rev-parse HEAD 2>/dev/null || true)"
if [[ -z "$tip" && -n "$lokal" ]]; then
  echo "[!] Ujung ${BRANCH} tidak terbaca dari remote — memakai sumber yang ada."
elif [[ "$lokal" != "$tip" ]]; then
  echo "[i] Mengambil ulang ${REPO} ke ${SRC} (ujung ${BRANCH}: ${tip:0:7})…"
  rm -rf "$SRC"
  git clone --depth 1 --branch "$BRANCH" "$REPO" "$SRC"
fi

echo "[i] Versi sumber: $(git -C "$SRC" log -1 --format='%h %s')"

# Binary lama dibuang supaya installer benar-benar build ulang: install.sh
# melewati langkah build kalau bin/ sudah terisi, dan itu justru membuat
# pembaruan memasang binary lama yang sama.
rm -rf "${SRC:?}/bin"

echo "[i] Menjalankan installer dari sumber baru…"
exec bash "${SRC}/deploy/install.sh"
