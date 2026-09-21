#!/bin/bash
# Skrip pembaruan panel — ditanam di binary helper (go:embed) dan ditulis ke
# /var/lib/linux-dashboard/update.sh saat tombol Update ditekan.
#
# Ditanam, bukan dibaca dari checkout, karena mesin yang belum pernah punya
# sumber di /usr/local/src pun harus bisa memulai pembaruan pertamanya.
#
# SASARANNYA SATU: ujung branch di remote. Berapa pun commit yang tertinggal —
# satu, lima, atau lima puluh — satu kali jalan berakhir persis di commit itu.
# Tidak ada langkah per-commit: yang dilakukan cuma menyegarkan sumber lalu
# menyerahkan sisanya ke deploy/install.sh versi BARU (langkah pasang service,
# user sistem, dan restart sudah ada di sana, jadi perubahan cara pasang ikut
# terbawa update tanpa perlu menyalin langkahnya ke sini).
#
# Karena itu skrip ini juga MENOLAK jalan kalau ujung branch tidak terbaca:
# meneruskan dengan sumber lama berarti memasang ulang versi yang sudah ada
# sambil melaporkan "selesai" — pembaruan yang sukses di layar tapi tidak
# memindahkan versi sama sekali.
set -euo pipefail

REPO="${REPO:-https://github.com/OxidiLily/linux-dashboard.git}"
BRANCH="${BRANCH:-main}"
SRC="${SRC:-/usr/local/src/go-react-linux-dashboard}"

# baca_tip membaca ujung branch di remote. Dipisah supaya bisa diulang sekali
# saat jaringan sedang tidak bersahabat, dan supaya kegagalannya tidak
# membingungkan dengan kegagalan `set -e`.
baca_tip() {
  git ls-remote "$REPO" "refs/heads/${BRANCH}" 2>/dev/null | cut -f1 || true
}

# Ujung branch dibaca lebih dulu, dan itulah satu-satunya sasaran: pembaruan
# baru dianggap berhasil kalau sumber berakhir persis di commit ini.
tip="$(baca_tip)"
if [[ -z "$tip" ]]; then
  echo "[!] Ujung ${BRANCH} tidak terbaca dari remote — mencoba sekali lagi…"
  tip="$(baca_tip)"
fi
if [[ -z "$tip" ]]; then
  lokal_skrg="$(git -C "$SRC" log -1 --format='%h %s' 2>/dev/null || echo 'tidak diketahui')"
  echo "[✗] Ujung ${BRANCH} tidak bisa dibaca dari ${REPO} — pembaruan DIBATALKAN." >&2
  echo "[✗] Tidak ada yang dipasang: panel tetap di versi ${lokal_skrg}." >&2
  echo "[i] Periksa jaringan/DNS mesin ini, lalu tekan Update lagi." >&2
  exit 1
fi
echo "[i] Sasaran pembaruan: ${tip:0:7} — ujung ${BRANCH}, dipasang sekaligus (bukan per commit)."

if [[ -d "$SRC/.git" ]] && git -C "$SRC" rev-parse --git-dir >/dev/null 2>&1; then
  echo "[i] Menyegarkan sumber di ${SRC} (branch ${BRANCH})…"
  # reset ke FETCH_HEAD, BUKAN ke origin/<branch>: remote-tracking ref hanya
  # ikut terbarui kalau refspec fetch checkout ini memang memetakannya, dan
  # checkout yang dibuat dengan cara lain akan selamanya reset ke ref lama
  # yang sama — pembaruan terlihat sukses tapi versinya tidak pernah pindah.
  # FETCH_HEAD selalu berisi apa yang baru saja diambil.
  #
  # --depth 1 tetap melompat ke ujung branch berapa pun jaraknya: yang diambil
  # adalah commit ujung itu sendiri sebagai akar dangkal yang baru, bukan satu
  # commit di depan HEAD.
  #
  # Jumlah commit yang dilompati sengaja TIDAK dihitung di sini. Checkout
  # dangkal tidak menyimpan nenek moyang, jadi `rev-list --count HEAD..FETCH_HEAD`
  # bukan jarak sesungguhnya — pada clone yang berakar di commit lama ia bahkan
  # menjawab "1" (hanya ujung yang ikut terunduh) atau melebihkan hitungan
  # sebanyak riwayat yang tidak ada di lokal. Angka yang butuh kejujuran itu
  # disediakan halaman Update dari daftar commit yang benar-benar diambil, dan
  # dikatakan "sekurang-kurangnya" kalau riwayat lokal tidak menyambung.
  if git -C "$SRC" fetch --depth 1 --force origin "$BRANCH"; then
    git -C "$SRC" reset --hard FETCH_HEAD
  fi
fi

# Apa pun yang membuat penyegaran di atas gagal — ref lama, berkas shallow
# rusak, fetch putus, sumber belum ada — dijawab dengan cara yang sama:
# ambil ulang dari nol. Lebih lambat, tapi tidak ada keadaan yang membuat
# panel diam-diam terus memasang versi lama.
lokal="$(git -C "$SRC" rev-parse HEAD 2>/dev/null || true)"
if [[ "$lokal" != "$tip" ]]; then
  echo "[i] Mengambil ulang ${REPO} ke ${SRC} (ujung ${BRANCH}: ${tip:0:7})…"
  rm -rf "$SRC"
  git clone --depth 1 --branch "$BRANCH" "$REPO" "$SRC"
fi

# Pemeriksaan terakhir sebelum apa pun dipasang: sumber harus benar-benar
# berakhir di ujung branch. Sampai sini seharusnya selalu terpenuhi (dua jalur
# di atas sama-sama menuju sana), dan justru karena itu ia diperiksa — kalau
# suatu saat tidak, yang terjadi adalah pembaruan yang memasang versi lain
# tanpa satu pun petunjuk di layar.
kepala="$(git -C "$SRC" rev-parse HEAD 2>/dev/null || true)"
if [[ "$kepala" != "$tip" ]]; then
  echo "[✗] Sumber berakhir di ${kepala:0:7}, bukan sasaran ${tip:0:7} — pembaruan DIBATALKAN sebelum memasang apa pun." >&2
  exit 1
fi
echo "[i] Versi sumber: $(git -C "$SRC" log -1 --format='%h %s') (sasaran ${tip:0:7} tercapai)"

# Binary lama dibuang supaya installer benar-benar build ulang: install.sh
# melewati langkah build kalau bin/ sudah terisi, dan itu justru membuat
# pembaruan memasang binary lama yang sama.
rm -rf "${SRC:?}/bin"

echo "[i] Menjalankan installer dari sumber baru…"
exec bash "${SRC}/deploy/install.sh"
