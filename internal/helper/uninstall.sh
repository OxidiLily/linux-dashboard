#!/bin/bash
# Uninstaller linux-dashboard — kebalikan deploy/install.sh.
#
#   sudo ./deploy/uninstall.sh panel        # binary, service, PAM, sumber
#   sudo ./deploy/uninstall.sh panel-data   # + data & config panel
#   sudo ./deploy/uninstall.sh total        # + copot components yang dipasang panel
#   sudo ./deploy/uninstall.sh total-data   # + hapus folder data akun (~/DATA)
#
# Dipanggil panel lewat helper daemon (Settings → profil → Uninstall), tapi
# tetap bisa dijalankan sendiri dari terminal.
#
# ATURAN:
# berkas pribadi user di ~/DATA/* TIDAK PERNAH dihapus mode mana pun KECUALI
# `total-data`. Folder itu dibuatkan panel, tapi isinya milik pemilik akun —
# dokumen, foto, dan (di mesin yang dipakai mengembangkan panel ini) checkout
# kode beserta vault catatan. Tidak ada tombol "undo" untuk penghapusan itu,
# jadi mode tersebut harus dipilih sendiri dan diketik ulang namanya di UI.
set -uo pipefail

MODE="${1:-panel}"
PREFIX="${PREFIX:-/usr/local/bin}"
SRC="${SRC:-/usr/local/src/go-react-linux-dashboard}"
SERVICE_USER="linux-dashboard"

log() { echo "[i] $*"; }
ok() { echo "[✓] $*"; }
die() { echo "[✗] $*" >&2; exit 1; }

case "$MODE" in
  panel | panel-data | total | total-data) ;;
  *) die "Mode tidak dikenal: ${MODE} (pakai panel | panel-data | total | total-data)" ;;
esac
[[ $EUID -eq 0 ]] || die "Harus root."

log "Mode: ${MODE}"

# ---- 1. Hentikan service -------------------------------------------------
# Dijalankan sebagai unit transient di luar cgroup panel (lihat uninstall.go),
# jadi menghentikan service sendiri di sini aman — skrip ini tidak ikut mati.
for unit in linux-dashboard-web linux-dashboard-helper smbd nmbd 9router headroom; do
  systemctl disable --now "${unit}.service" >/dev/null 2>&1 || true
done
ok "Service panel dan layanan terkait dihentikan & di-disable"

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
if [[ "$MODE" == "total" || "$MODE" == "total-data" ]]; then
  if [[ -x "${PREFIX}/linux-dashboard-helper" ]]; then
    log "Mencopot components yang dipasang panel…"
    "${PREFIX}/linux-dashboard-helper" copot-components ||
      echo "[⚠] Sebagian components gagal dicopot — cek daftarnya di atas" >&2
    ok "Components selesai diproses"
  else
    echo "[⚠] Binary helper tidak ada — components dilewati, copot manual lewat apt" >&2
  fi

  # Pembersihan sapu jagat Docker (container, volume, network, image, config)
  log "Membersihkan sisa container, volume, network, dan image Docker…"
  if command -v docker >/dev/null 2>&1; then
    running_c=$(docker ps -q 2>/dev/null || true)
    if [[ -n "$running_c" ]]; then
      # shellcheck disable=SC2086
      docker stop -t 5 $running_c >/dev/null 2>&1 || true
    fi
    all_c=$(docker ps -aq 2>/dev/null || true)
    if [[ -n "$all_c" ]]; then
      # shellcheck disable=SC2086
      docker rm -f $all_c >/dev/null 2>&1 || true
    fi
    docker volume prune -a -f >/dev/null 2>&1 || true
    docker network prune -f >/dev/null 2>&1 || true
    docker system prune -a --volumes -f >/dev/null 2>&1 || true
  fi
  for unit in docker docker.socket containerd 9router headroom smbd nmbd; do
    systemctl disable --now "${unit}.service" >/dev/null 2>&1 || true
  done
  rm -rf /var/lib/docker /var/lib/containerd /etc/docker /var/run/docker.sock /var/run/docker
  rm -rf /opt/supabase /opt/arkon /opt/headroom /opt/pipx /opt/dotnet
  rm -f /usr/local/bin/9router /usr/bin/9router /usr/local/bin/headroom /usr/local/bin/rtk /usr/local/bin/graphify /usr/local/bin/browser-use*
  rm -rf /usr/local/lib/hermes-agent /usr/lib/node_modules/9router /usr/local/lib/node_modules/9router
  rm -f /etc/systemd/system/9router.service /etc/systemd/system/headroom.service
  rm -rf /etc/systemd/system/9router.service.d /etc/systemd/system/headroom.service.d
  systemctl daemon-reload >/dev/null 2>&1 || true

  # Hapus paket build/runtime/komponen yang dipasang installer/panel jika ada
  if command -v apt-get >/dev/null 2>&1; then
    export DEBIAN_FRONTEND=noninteractive
    apt-get purge -y --auto-remove samba smbd nmbd samba-common samba-common-bin >/dev/null 2>&1 || true
    apt-get remove -y -qq golang-go nodejs npm >/dev/null 2>&1 || true
    apt-get autoremove -y -qq >/dev/null 2>&1 || true
  fi

  # Sapu bersih file dan folder komponen di SELURUH home akun manusia dan /root
  log "Membersihkan jejak direktori dan cache komponen di seluruh akun…"
  user_homes=("/root")
  while IFS=: read -r _nama _sandi _uid _gid _gecos home shell; do
    [[ -n "$home" && "$home" != "/" ]] || continue
    case "$shell" in ""|*/nologin|*/false|*/sync) continue ;; esac
    user_homes+=("${home%/}")
  done < <(getent passwd)

  for uh in "${user_homes[@]}"; do
    [[ -d "$uh" ]] || continue
    rm -rf "$uh/.9router" \
           "$uh/.docker" \
           "$uh/.headroom" \
           "$uh/.hermes" \
           "$uh/.cua-driver" \
           "$uh/.npm" \
           "$uh/.npm-global" \
           "$uh/go" \
           "$uh/.claude" "$uh/.claude.json" \
           "$uh/.codex" \
           "$uh/.opencode" \
           "$uh/.openclaw" \
           "$uh/.config/linux-dashboard" \
           "$uh/.config/rtk" \
           "$uh/.config/graphify" \
           "$uh/.config/ponytail" \
           "$uh/.config/browser-harness" \
           "$uh/.config/google-chrome-for-testing" \
           "$uh/.config/go" \
           "$uh/.local/share/rtk" \
           "$uh/.local/share/claude" \
           "$uh/.local/share/opencode" \
           "$uh/.local/share/hermes" \
           "$uh/.local/state/hermes" \
           "$uh/.local/pipx" \
           "$uh/.local/state/pipx" \
           "$uh/.cache/ms-playwright" \
           "$uh/.cache/go-build" \
           "$uh/.cache/goimports" \
           "$uh/.cache/gopls" \
           "$uh/.cache/claude" \
           "$uh/.cache/claude-cli-nodejs" \
           "$uh/.cache/codex" \
           "$uh/.cache/opencode" \
           "$uh/.cache/hermes" \
           "$uh/.cache/node" \
           "$uh/.cache/npm"
    rm -f "$uh/.local/bin/hermes" "$uh/.local/bin/claude" "$uh/.local/bin/codex" "$uh/.local/bin/rtk" "$uh/.local/bin/opencode"
  done
  ok "Pembersihan komponen dan jejak akun selesai"
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
  # Hapus juga config panel per-user di .config/linux-dashboard
  while IFS=: read -r _nama _sandi _uid _gid _gecos home shell; do
    [[ -n "$home" && "$home" != "/" && -d "$home" ]] || continue
    rm -rf "${home%/}/.config/linux-dashboard"
  done < <(getent passwd)
  rm -rf /root/.config/linux-dashboard

  # Bersihkan konfigurasi Samba panel
  rm -f /etc/samba/lindash-shares.conf
  if [[ -f /etc/samba/smb.conf.lindash.bak ]]; then
    cp /etc/samba/smb.conf.lindash.bak /etc/samba/smb.conf 2>/dev/null || true
    rm -f /etc/samba/smb.conf.lindash.bak
  elif [[ -f /etc/samba/smb.conf ]]; then
    sed -i '/# ditambahkan oleh linux-dashboard/d' /etc/samba/smb.conf 2>/dev/null || true
    sed -i '/include = \/etc\/samba\/lindash-shares.conf/d' /etc/samba/smb.conf 2>/dev/null || true
    sed -i '/# ---- linux-dashboard: audit autentikasi/,+10d' /etc/samba/smb.conf 2>/dev/null || true
  fi
  rm -f /var/lib/samba/private/passdb.tdb /var/lib/samba/private/secrets.tdb /var/lib/samba/passdb.tdb

  # Bersihkan konfigurasi fail2ban panel
  rm -f /etc/fail2ban/jail.d/lindash-*.conf /etc/fail2ban/filter.d/lindash-*.conf
  if command -v fail2ban-client >/dev/null 2>&1; then
    fail2ban-client reload >/dev/null 2>&1 || true
  fi

  # Sisa rilis panel lama yang mengelola WireGuard: berkas ini ditulis panel
  # (nama berkasnya berawalan linux-dashboard), jadi ia tetap dibersihkan
  # meski komponen WireGuard sudah tidak ada lagi di katalog. Konfigurasi
  # /etc/wireguard sendiri TIDAK disentuh — sejak WireGuard bukan komponen
  # panel, isinya bukan lagi milik panel untuk dihapus.
  rm -f /etc/sysctl.d/99-linux-dashboard-wg.conf

  # Bersihkan konfigurasi NFS panel
  rm -f /etc/exports.d/lindash.exports
  if [[ -f /etc/exports ]]; then
    sed -i '/# lindash-nfs/d' /etc/exports 2>/dev/null || true
  fi
  if command -v exportfs >/dev/null 2>&1; then
    exportfs -ra 2>/dev/null || true
  fi

  # Bersihkan mount point NFS dan mergerfs panel di /etc/fstab
  if [[ -f /etc/fstab ]]; then
    sed -i '/# lindash-nfsmount/d' /etc/fstab 2>/dev/null || true
    sed -i '/# lindash-mergerfs/d' /etc/fstab 2>/dev/null || true
  fi

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

# ---- 4b. Folder data akun (HANYA mode total-data) ------------------------
# Bagian yang mode lain tidak pernah sentuh. Dipisah dari bagian 3 justru
# supaya tidak bisa terpanggil diam-diam: `panel` dan `panel-data` mengganti
# panel, bukan isi mesin.
#
# Yang dihapus hanya `DATA` di dalam home, tidak pernah home-nya sendiri, dan
# akun service (shell nologin) dilewati karena tidak pernah punya folder ini.
# Pagar kedua di bawah ini adalah yang terakhir: kalau path-nya bukan
# `<home>/DATA` persis, penghapusan dibatalkan.
if [[ "$MODE" == "total-data" ]]; then
  dihapus=0
  while IFS=: read -r _nama _sandi _uid _gid _gecos home shell; do
    [[ -n "$home" && "$home" != "/" ]] || continue
    case "$shell" in ""|*/nologin|*/false|*/sync) continue ;; esac
    target="${home%/}/DATA"
    # Pagar terakhir: hanya path yang PERSIS <home>/DATA yang boleh dihapus.
    if [[ "$target" == "/DATA" || "$target" != */DATA ]]; then
      echo "[⚠] Path ${target} tidak berbentuk <home>/DATA — dilewati" >&2
      continue
    fi
    if [[ -e "$target" ]]; then
      log "Menghapus data akun ${_nama}: ${target}"
      rm -rf --one-file-system "$target"
      dihapus=$((dihapus + 1))
    fi
  done < <(getent passwd)
  # /etc/skel: kerangka folder data untuk akun yang dibuat BELAKANGAN.
  rm -rf --one-file-system /etc/skel/DATA
  ok "${dihapus} folder data akun dihapus, /etc/skel/DATA dibereskan"
  echo "[⚠] Folder di atas TIDAK bisa dikembalikan — dokumen, foto, dan kode di dalamnya hilang." >&2
fi

# ---- 5. Yang sengaja ditinggalkan ---------------------------------------
if [[ "$MODE" == "total-data" ]]; then
  echo "[i] Folder data akun (~/DATA/*) sudah dihapus atas permintaan mode total-data."
else
  echo "[i] Folder data akun (~/DATA/*) TIDAK dihapus — isinya milik pemilik akun."
fi
ok "Layanan dan konfigurasi yang dikelola panel (Samba, NFS, 9router, Headroom) telah dibersihkan."
ok "Uninstall selesai (mode ${MODE})."
