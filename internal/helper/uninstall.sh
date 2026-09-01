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

# Binary helper sengaja BELUM dihapus di sini: mode "total" memakainya untuk
# mencopot components (bagian 2), dan uninstaller komponen yang sesungguhnya
# ada di dalam binary itu. Penghapusannya menyusul di bagian 4.
rm -f "${PREFIX}/linux-dashboard-server"
rm -f /etc/pam.d/linux-dashboard
rm -rf "$SRC"
ok "Binary web, konfigurasi PAM, dan sumber dihapus"

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

# ---- 2. Components ------------------------------------------------------
# Dijalankan SEBELUM data panel dihapus: penanda status sebagian komponen
# tinggal di /var/lib/linux-dashboard (ponytail), dan komponen yang penandanya
# sudah lenyap terbaca "belum terpasang" lalu dilewati — pendaftaran plugin di
# tiap agent tidak pernah dicabut.
# Pencopotannya dijalankan binary helper (mode copot-components), bukan daftar
# paket yang ditulis ulang di sini. Daftar bash tidak pernah ikut bertambah
# saat katalog di internal/helper/components.go bertambah — itulah kenapa
# docker, Node.js, Tailscale, cloudflared, dan seluruh alat AI sempat tetap
# terpasang setelah user memilih "hapus total". Helper juga tahu hal yang
# tidak diketahui `apt remove`: repo & keyring vendor, unit systemd cloudflared
# beserta token tunnelnya, paket npm global, dan pipx.
if [[ "$MODE" == "total" ]]; then
  if [[ -x "${PREFIX}/linux-dashboard-helper" ]]; then
    log "Mencopot components yang dipasang panel…"
    "${PREFIX}/linux-dashboard-helper" copot-components ||
      echo "[⚠] Sebagian components gagal dicopot — cek daftarnya di atas" >&2
    ok "Components selesai diproses"
  else
    echo "[⚠] Binary helper tidak ada — components dilewati, copot manual lewat apt" >&2
  fi
fi

# ---- 3. Data & config panel ---------------------------------------------
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
  #
  # Pagar UID + shell: yang boleh dihapus HANYA akun sistem buatan installer
  # (`useradd --system --no-create-home --shell /usr/sbin/nologin`), yaitu
  # UID < 1000 dengan shell nologin/false. Kalau di mesin ini "linux-dashboard"
  # ternyata akun manusia dengan home dan shell sungguhan, uninstall panel
  # tidak berhak menghapusnya — dan tidak ada cara mengembalikan akun yang
  # sudah dihapus. `userdel` juga dipanggil tanpa `-r`, jadi home directory
  # tidak pernah ikut terhapus meski akunnya lolos pagar.
  if uid=$(id -u "$SERVICE_USER" 2>/dev/null); then
    shell=$(getent passwd "$SERVICE_USER" | cut -d: -f7)
    if (( uid < 1000 )) && [[ "$shell" == */nologin || "$shell" == */false ]]; then
      userdel --force "$SERVICE_USER" >/dev/null 2>&1
      ok "Akun sistem ${SERVICE_USER} dihapus"
    else
      echo "[⚠] Akun ${SERVICE_USER} (uid ${uid}, shell ${shell}) bukan akun sistem buatan installer — TIDAK dihapus" >&2
    fi
  fi
fi

# ---- 4. Binary helper ---------------------------------------------------
rm -f "${PREFIX}/linux-dashboard-helper"
ok "Binary helper dihapus"

# ---- 5. Yang sengaja ditinggalkan ---------------------------------------
echo "[i] Folder data akun (~/DATA/*) TIDAK dihapus — isinya milik pemilik akun."
echo "[i] Konfigurasi layanan di luar panel (Samba, NFS, WireGuard, firewall) tetap apa adanya."
ok "Uninstall selesai (mode ${MODE})."
