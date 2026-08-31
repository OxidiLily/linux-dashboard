#!/bin/bash
# Uninstaller linux-dashboard — kebalikan deploy/install.sh.
#
#   sudo ./deploy/uninstall.sh panel        # binary, service, PAM, sumber
#   sudo ./deploy/uninstall.sh panel-data   # + data & config panel
#   sudo ./deploy/uninstall.sh total        # + copot components yang dipasang panel
#
# Dipanggil panel lewat helper daemon (Settings → profil → Uninstall), tapi
# tetap bisa dijalankan sendiri dari terminal.
#
# ATURAN YANG TIDAK PERNAH DILANGGAR MODE MANA PUN:
# berkas pribadi user di ~/DATA/* TIDAK PERNAH dihapus. Folder itu memang
# pernah dibuatkan panel, tapi isinya milik pemilik akun — dokumen, foto,
# media — dan tidak ada tombol "undo" untuk penghapusan itu.
set -uo pipefail

MODE="${1:-panel}"
PREFIX="${PREFIX:-/usr/local/bin}"
SRC="${SRC:-/usr/local/src/go-react-linux-dashboard}"
SERVICE_USER="linux-dashboard"

log() { echo "[i] $*"; }
ok() { echo "[✓] $*"; }
die() { echo "[✗] $*" >&2; exit 1; }

case "$MODE" in
  panel | panel-data | total) ;;
  *) die "Mode tidak dikenal: ${MODE} (pakai panel | panel-data | total)" ;;
esac
[[ $EUID -eq 0 ]] || die "Harus root."

log "Mode: ${MODE}"

# ---- 1. Hentikan service -------------------------------------------------
# Dijalankan sebagai unit transient di luar cgroup panel (lihat uninstall.go),
# jadi menghentikan service sendiri di sini aman — skrip ini tidak ikut mati.
for unit in linux-dashboard-web linux-dashboard-helper; do
  systemctl disable --now "${unit}.service" >/dev/null 2>&1
done
ok "Service dihentikan & di-disable"

rm -f /etc/systemd/system/linux-dashboard-web.service \
      /etc/systemd/system/linux-dashboard-helper.service
systemctl daemon-reload
ok "Unit systemd dihapus"

rm -f "${PREFIX}/linux-dashboard-server" "${PREFIX}/linux-dashboard-helper"
rm -f /etc/pam.d/linux-dashboard
rm -rf "$SRC"
ok "Binary, konfigurasi PAM, dan sumber dihapus"

# ---- 1b. Command CLI -----------------------------------------------------
# Mode 'panel' mempertahankan `uninstall-linuxpanel` supaya user bisa reinstall
# tanpa harus unduh installer dari GitHub dulu. Mode 'panel-data' / 'total'
# menghapus command ini juga — reinstall akan menulisnya kembali.
if [[ "$MODE" == "panel" ]]; then
  : # sengaja dibiarkan
else
  rm -f "${PREFIX}/uninstall-linuxpanel"
  ok "Command uninstall-linuxpanel dihapus"
fi

# ---- 2. Data & config panel ---------------------------------------------
if [[ "$MODE" != "panel" ]]; then
  # Database panel (akun panel, bookmark, threshold, log aktivitas), kunci
  # sesi, dan berkas kerja pembaruan. Tidak ada di sini yang bisa dipulihkan.
  rm -rf /var/lib/linux-dashboard /var/lib/linux-dashboard-update
  rm -f /etc/sysctl.d/99-linux-dashboard-wg.conf
  # Setelan per-device dan sertifikat sengaja ikut mode ini saja: pada mode
  # "panel" panel dipasang ulang nanti, dan port, secure cookie, serta
  # sertifikat pilihan pemilik mesin harus masih ada saat itu.
  rm -f /etc/default/linux-dashboard
  rm -rf /etc/linux-dashboard
  ok "Data & config panel dihapus"

  # Akun service dihapus belakangan: selama /var/lib masih ada, folder itu
  # miliknya. --force supaya proses sisa tidak menggagalkan penghapusan.
  if id -u "$SERVICE_USER" >/dev/null 2>&1; then
    userdel --force "$SERVICE_USER" >/dev/null 2>&1
    ok "Akun sistem ${SERVICE_USER} dihapus"
  fi
fi

# ---- 3. Components ------------------------------------------------------
# Daftarnya sengaja sama dengan katalog di internal/helper/components.go —
# hanya software yang memang dipasang lewat halaman Components.
if [[ "$MODE" == "total" ]]; then
  paket=(samba mergerfs nfs-kernel-server ufw fail2ban wireguard wireguard-tools)
  ada=()
  for p in "${paket[@]}"; do
    dpkg-query -W -f='${Status}' "$p" 2>/dev/null | grep -q "ok installed" && ada+=("$p")
  done
  if (( ${#ada[@]} > 0 )); then
    log "Mencopot components: ${ada[*]}"
    DEBIAN_FRONTEND=noninteractive apt-get remove -y -qq "${ada[@]}" >/dev/null 2>&1 &&
      ok "Components dicopot" || echo "[⚠] Sebagian components gagal dicopot — cek dengan apt" >&2
  else
    log "Tidak ada component apt yang terpasang"
  fi

  # docker, nodejs, tailscale, cloudflared SENGAJA tidak ikut dicopot:
  # keempatnya lazim dipakai hal lain di mesin yang sama, dan mencabutnya bisa
  # menghancurkan container, service, atau tunnel yang tidak ada hubungannya
  # dengan panel. Copot manual lewat apt kalau memang mau.
  echo "[i] docker, nodejs, tailscale, dan cloudflared dibiarkan — copot manual kalau perlu."
fi

# ---- 4. Yang sengaja ditinggalkan ---------------------------------------
echo "[i] Folder data akun (~/DATA/*) TIDAK dihapus — isinya milik pemilik akun."
echo "[i] Konfigurasi layanan di luar panel (Samba, NFS, WireGuard, firewall) tetap apa adanya."
ok "Uninstall selesai (mode ${MODE})."
