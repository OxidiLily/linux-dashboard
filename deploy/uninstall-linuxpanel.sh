#!/bin/bash
# uninstall-linuxpanel — CLI entry untuk menghapus linux-dashboard.
#
# Dipakai saat panel tidak bisa dibuka dari browser (panel crash, lupa
# password, atau uninstall sengaja via SSH tanpa harus login web dulu).
# Sama dengan tombol Uninstall di Settings → Akun, hanya saja tanpa UI:
# pilih mode, konfirmasi password PAM, lalu binary mati sendiri.
#
# Pemakaian:
#   sudo uninstall-linuxpanel                # tanya mode, default 'panel'
#   sudo uninstall-linuxpanel --help         # tampilkan bantuan + keluar
#   sudo uninstall-linuxpanel panel          # hapus binary + service
#   sudo uninstall-linuxpanel panel-data     # + database & konfigurasi
#   sudo uninstall-linuxpanel total          # + copot components apt
#   sudo uninstall-linuxpanel -y panel       # lewati konfirmasi "y/n"
#
# Skrip uninstaller inti (internal/helper/uninstall.sh) dibaca dari
# /usr/local/share/linux-dashboard/ — installer meletakkannya di sana saat
# 'make install'. Tanpa berkas itu, command ini menolak jalan dengan pesan
# jelas; ini terjadi kalau linux-dashboard dipasang dari paket yang tidak
# menyertakan skrip uninstall.
#
# TIDAK menghapus: ~/DATA/* user, konfigurasi Samba/NFS/WireGuard/firewall,
# docker, nodejs, tailscale, cloudflared.
set -uo pipefail

PREFIX="${PREFIX:-/usr/local/bin}"
SHARE_DIR="${SHARE_DIR:-/usr/local/share/linux-dashboard}"
SERVICE_USER="linux-dashboard"
UNINSTALL_SH="${SHARE_DIR}/uninstall.sh"
LOG="/var/log/linux-dashboard-uninstall.log"

usage() {
  cat <<EOF
uninstall-linuxpanel — cabut linux-dashboard dari mesin ini.

Pemakaian:
  uninstall-linuxpanel                  Tanya mode (default: panel)
  uninstall-linuxpanel <mode>           Jalankan dengan mode tertentu
  uninstall-linuxpanel -y <mode>        Lewati konfirmasi y/n
  uninstall-linuxpanel --help           Tampilkan bantuan ini

Mode:
  panel        Hapus service, unit systemd, binary, PAM, source tree.
               Database panel & bookmark TETAP ADA.
  panel-data   Semua di atas + database panel, kunci sesi, sertifikat TLS,
               /etc/default/linux-dashboard, akun service linux-dashboard.
  total        Semua di atas + apt-copot Samba, mergerfs, NFS, ufw,
               Fail2ban, WireGuard. (Docker/Node/Tailscale/cloudflared
               dibiarkan — terlalu sering dipakai hal lain.)

Dipanggil tanpa sudo: otomatis re-exec lewat sudo. Installernya hanya
menaruh command ini di /usr/local/bin; tidak ada symlink di tempat lain.
EOF
}

log() { echo "[i] $*"; }
# shellcheck disable=SC2329
ok()  { echo "[✓] $*"; }
die() { echo "[✗] $*" >&2; exit 1; }

# ---- Re-exec lewat sudo kalau bukan root ----------------------------------
if [[ $EUID -ne 0 ]]; then
  command -v sudo >/dev/null 2>&1 ||
    die "Bukan root dan sudo tidak terpasang. Masuk sebagai root (mis. \`su -\`) lalu ulangi."
  exec sudo "$0" "$@"
fi

# ---- Parse argumen -------------------------------------------------------
ASSUME_YES=0
MODE_RAW=""
for arg in "$@"; do
  case "$arg" in
    -h|--help) usage; exit 0 ;;
    -y|--yes)  ASSUME_YES=1 ;;
    panel|panel-data|total) MODE_RAW="$arg" ;;
    *) die "Argumen tidak dikenal: '$arg'. Coba --help." ;;
  esac
done

# Saat uninstall-linuxpanel sendiri dihapus oleh mode 'panel-data' / 'total',
# installer meletakkannya kembali di tempat yang sama supaya command ini
# selalu tersedia. Pemeriksaan ini cuma peringatan — kalau installer tidak
# menulis ulang, command akan hilang setelah uninstall, sesuai desain.
[[ -x "$0" ]] || log "Catatan: \$0 tidak bisa di-stat — mungkin executable sudah dihapus."

# ---- Cari skrip uninstaller inti -----------------------------------------
if [[ ! -f "$UNINSTALL_SH" ]]; then
  # Fallback: pakai skrip yang di samping binary helper. Installasi standar
  # menulis uninstall.sh di dua tempat (sistem share + share binary) supaya
  # salah satu masih ada saat yang lain dihapus.
  for kandidat in \
    "${PREFIX}/../share/linux-dashboard/uninstall.sh" \
    "/usr/local/share/linux-dashboard/uninstall.sh" \
    "$(dirname "$(readlink -f "$0" 2>/dev/null || echo "$0")")/../share/linux-dashboard/uninstall.sh"
  do
    if [[ -f "$kandidat" ]]; then
      UNINSTALL_SH="$kandidat"
      break
    fi
  done
fi
[[ -f "$UNINSTALL_SH" ]] ||
  die "Berkas uninstall.sh tidak ditemukan di $SHARE_DIR — linux-dashboard tidak terpasang lewat installer standar?"

# ---- Konfirmasi interaktif kalau mode belum dipilih ----------------------
if [[ -z "$MODE_RAW" ]]; then
  echo "Pilih mode uninstall:"
  echo "  1) panel         — binary + service + PAM + unit systemd"
  echo "  2) panel-data    — + database, sertifikat TLS, /etc/default"
  echo "  3) total         — + apt-copot components yang dipasang panel"
  read -r -p "Mode [1/2/3, default 1]: " pilih
  case "${pilih:-1}" in
    1|"") MODE_RAW="panel" ;;
    2)    MODE_RAW="panel-data" ;;
    3)    MODE_RAW="total" ;;
    *)    die "Pilihan tidak dikenal: $pilih" ;;
  esac
fi

log "Mode: $MODE_RAW"

# ---- Konfirmasi y/n (skip kalau -y) --------------------------------------
if (( ASSUME_YES == 0 )); then
  echo
  echo "Akan menjalankan uninstall dengan mode '$MODE_RAW'."
  case "$MODE_RAW" in
    panel)
      echo "  — service dihentikan, binary & unit systemd dihapus"
      echo "  — database panel, akun, bookmark TETAP ADA"
      ;;
    panel-data)
      echo "  — semua mode panel"
      echo "  — database panel, kunci sesi, sertifikat TLS dihapus"
      echo "  — akun service '$SERVICE_USER' dihapus"
      ;;
    total)
      echo "  — semua mode panel-data"
      echo "  — Samba, mergerfs, NFS, ufw, Fail2ban, WireGuard dicopot"
      echo "  — Docker/Node/Tailscale/cloudflared DIBIARKAN"
      ;;
  esac
  echo "  — ~/DATA/ setiap akun TIDAK disentuh"
  echo
  read -r -p "Lanjut? Ketik 'yes' untuk konfirmasi: " jawab
  [[ "$jawab" == "yes" ]] || { log "Dibatalkan."; exit 0; }
fi

# ---- Eksekusi -------------------------------------------------------------
mkdir -p "$(dirname "$LOG")" 2>/dev/null || true
{
  echo "[i] uninstall-linuxpanel dipanggil pada $(date -Iseconds)"
  echo "[i] Mode: $MODE_RAW"
} >>"$LOG" 2>/dev/null || true

# Jalankan uninstaller inti dengan env yang konsisten.
PREFIX="$PREFIX" \
  SRC="${SRC:-/usr/local/src/go-react-linux-dashboard}" \
  SHARE_DIR="$SHARE_DIR" \
  bash "$UNINSTALL_SH" "$MODE_RAW"
rc=$?

echo "[i] Log uninstall: $LOG"
exit "$rc"