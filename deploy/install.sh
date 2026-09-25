#!/bin/bash
# Installer linux-dashboard — dua mode, satu skrip:
#
#   1. Satu baris (tanpa clone manual):
#        curl -fsSL https://raw.githubusercontent.com/OxidiLily/linux-dashboard/main/deploy/install.sh | sudo bash
#      Skrip memasang dependency build, mengambil sumber ke /usr/local/src,
#      build UI + dua binary, lalu memasang service.
#
#   2. Dari checkout repo (dipakai `make install`):
#        sudo ./deploy/install.sh
#      Kalau bin/ sudah ada hasil `make build`, langkah build dilewati.
#
# Variabel yang bisa di-override: REPO, BRANCH, SRC, PREFIX.
set -euo pipefail

REPO="${REPO:-https://github.com/OxidiLily/linux-dashboard.git}"
BRANCH="${BRANCH:-main}"
SRC="${SRC:-/usr/local/src/go-react-linux-dashboard}"
PREFIX="${PREFIX:-/usr/local/bin}"
SERVICE_USER="linux-dashboard"
# Sama dengan komponen nodejs di panel (internal/helper/components.go).
NODE_MAJOR="${NODE_MAJOR:-24}"
# INITIAL_PASSWORD: kalau diset, tiap akun Linux non-sistem (UID ≥ 1000 dan
# bukan nologin) yang TIDAK punya password akan diberi password ini. Dipakai
# untuk instalasi headless lewat cloud-init/auto-install — tanpa ini, panel
# menolak login karena PAM tidak menemukan kredensial. Setelah login pertama
# user WAJIB ganti password (lihat enforce_initial_password di komponen UI).
INITIAL_PASSWORD="${INITIAL_PASSWORD:-}"

log() { echo "[i] $*"; }
ok() { echo "[✓] $*"; }
die() { echo "[✗] $*" >&2; exit 1; }

# ---- 0. Naik ke root -----------------------------------------------------
# Installer butuh root (pasang binary, unit systemd, user sistem). Kalau
# dijalankan sebagai user biasa, skrip menaikkan dirinya sendiri lewat sudo
# alih-alih menyuruh user mengetik ulang perintahnya.
#
# Dua batasan yang menentukan bentuk kode di bawah:
#   1. Versi yang dipipe dari curl datang lewat stdin — tidak ada berkas yang
#      bisa dieksekusi ulang, jadi satu-satunya jalan adalah user memakai
#      `| sudo bash` sendiri.
#   2. Variabel override diteruskan satu per satu, bukan lewat `sudo -E`:
#      sudoers dengan env_reset (bawaan Debian/Ubuntu) menolak -E untuk user
#      yang tidak punya flag SETENV, dan skrip akan mati sebelum mulai.
if [[ $EUID -ne 0 ]]; then
  command -v sudo >/dev/null 2>&1 ||
    die "Bukan root dan sudo tidak terpasang. Masuk sebagai root (mis. \`su -\`) lalu ulangi — sudo akan dipasang otomatis dari sana."
  [[ -f "${BASH_SOURCE[0]:-}" ]] ||
    die "Jalankan lewat sudo: curl -fsSL <url> | sudo bash"
  log "Bukan root — mengulang lewat sudo…"
  exec sudo REPO="$REPO" BRANCH="$BRANCH" SRC="$SRC" PREFIX="$PREFIX" \
    NODE_MAJOR="$NODE_MAJOR" ${BIN_SRC:+BIN_SRC="$BIN_SRC"} \
    bash "${BASH_SOURCE[0]}" "$@"
fi

if ! grep -qiE 'ubuntu|debian' /etc/os-release; then
  echo "[⚠] Target resmi hanya Ubuntu/Debian. Lanjut atas risiko sendiri." >&2
fi

# ---- 1. Tentukan root repo ----------------------------------------------
# Saat di-pipe ke bash, $0 bukan path file sehingga repo harus di-clone dulu.
REPO_ROOT=""
if [[ -f "${BASH_SOURCE[0]:-}" ]]; then
  candidate=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
  [[ -f "$candidate/go.mod" ]] && REPO_ROOT="$candidate"
fi

# paket_terpasang memakai dpkg-query, bukan `command -v`: beberapa dependency
# (libpam0g-dev) tidak punya binary sama sekali.
paket_terpasang() {
  dpkg-query -W -f='${Status}' "$1" 2>/dev/null | grep -q "ok installed"
}

# sudo bukan cuma alat menjalankan installer ini: model otorisasi panel membaca
# keanggotaan grup `sudo` (lihat lookupUser di internal/helper/authz.go), dan
# grup itu dibuat oleh paket sudo. Di image minimal (container, debootstrap)
# paketnya sering tidak ada — tanpa itu tidak ada satu pun akun yang dianggap
# sudoer oleh panel. Deteksi dulu, pasang hanya kalau memang belum ada.
pastikan_sudo() {
  if command -v sudo >/dev/null 2>&1; then
    return
  fi
  log "sudo belum terpasang — memasang…"
  export DEBIAN_FRONTEND=noninteractive
  apt-get update -qq
  apt-get install -y -qq sudo || die "Gagal memasang sudo — pasang manual: apt-get install sudo"
  ok "sudo dipasang"
  getent group sudo >/dev/null || echo "[⚠] Grup sudo tidak ada setelah pemasangan — panel tidak akan mengenali akun sudoer." >&2
}

pastikan_sudo

# UFW dan fail2ban adalah lapisan keamanan dasar, bukan fitur opsional. Paket
# yang sudah ada tidak disentuh; apt hanya menerima nama yang belum terpasang.
pastikan_keamanan_runtime() {
  export DEBIAN_FRONTEND=noninteractive
  # acl menyediakan getfacl/setfacl untuk hak per-share tanpa chown folder.
  # util-linux menyediakan mountpoint untuk mencegah ACL menyeberang ke nested
  # filesystem/bind mount di dalam root share.
  local wajib=(openssl ufw fail2ban acl util-linux)
  local kurang=()
  local p
  for p in "${wajib[@]}"; do
    paket_terpasang "$p" || kurang+=("$p")
  done
  if (( ${#kurang[@]} == 0 )); then
    ok "OpenSSL, UFW, fail2ban, ACL, dan util-linux sudah terpasang"
    return
  fi
  log "Memasang keamanan runtime yang belum ada: ${kurang[*]}"
  apt-get update -qq
  apt-get install -y -qq --no-install-recommends "${kurang[@]}" || \
    die "Gagal memasang keamanan runtime: ${kurang[*]}"
}

install_build_deps() {
  export DEBIAN_FRONTEND=noninteractive
  # Deteksi dulu, pasang seperlunya. Menjalankan apt-get install untuk paket
  # yang sudah ada bukan cuma lambat: `apt-get update` menarik seluruh index
  # repo (dan gagal kalau salah satu repo pihak ketiga sedang bermasalah)
  # padahal tidak ada yang perlu dipasang.
  local wajib=(ca-certificates curl git make build-essential libpam0g-dev)
  local kurang=()
  for p in "${wajib[@]}"; do
    paket_terpasang "$p" || kurang+=("$p")
  done
  # Go sering dipasang di luar apt (tarball resmi, asdf, snap). Cek binary-nya,
  # bukan paket golang-go — kalau tidak, toolchain yang sudah ada diabaikan dan
  # apt memasang Go kedua yang tidak pernah dipakai.
  command -v go >/dev/null 2>&1 || kurang+=(golang-go)
  if (( ${#kurang[@]} == 0 )); then
    ok "Dependency build sudah lengkap — tidak ada yang dipasang"
  else
    log "Memasang dependency build yang belum ada: ${kurang[*]}"
    apt-get update -qq
    # golang-go boleh versi lawas: go.mod menuntut Go 1.26.6 dan toolchain itu
    # diunduh sendiri oleh Go (GOTOOLCHAIN=auto, diset sebelum `make build`).
    # libpam0g-dev wajib — helper daemon memakai PAM lewat cgo.
    apt-get install -y -qq --no-install-recommends "${kurang[@]}"
  fi

  # Node diambil dari NodeSource, bukan apt: Ubuntu 24.04 mengirim Node 18 +
  # npm 9 yang kena bug optional dependency npm (npm/cli#4828) sehingga binding
  # native @tailwindcss/oxide tidak ikut terpasang dan `vite build` gagal.
  # Paket `npm` distro juga menyeret ~200 paket node-* yang tidak terpakai.
  local node_major
  node_major=$(node -p 'process.versions.node.split(".")[0]' 2>/dev/null || echo 0)
  if (( node_major < 20 )); then
    log "Node ${node_major} terlalu lawas → memasang Node ${NODE_MAJOR} dari NodeSource…"
    curl -fsSL "https://deb.nodesource.com/setup_${NODE_MAJOR}.x" | bash -
    apt-get install -y -qq nodejs
  fi
}

if [[ -z "$REPO_ROOT" ]]; then
  command -v apt-get >/dev/null || die "Butuh Debian/Ubuntu (apt-get tidak ada)."
  install_build_deps
  if [[ -d "$SRC/.git" ]]; then
    log "Memperbarui sumber di ${SRC}…"
    git -C "$SRC" fetch --depth 1 origin "$BRANCH"
    git -C "$SRC" reset --hard "origin/${BRANCH}"
  else
    log "Mengambil sumber ke ${SRC}…"
    rm -rf "$SRC"
    git clone --depth 1 --branch "$BRANCH" "$REPO" "$SRC"
  fi
  REPO_ROOT="$SRC"
fi

cd "$REPO_ROOT"

command -v apt-get >/dev/null || die "Butuh Debian/Ubuntu (apt-get tidak ada)."
pastikan_keamanan_runtime

# ---- 2. Build kalau binary belum ada ------------------------------------
BIN_SRC_DIBERIKAN="${BIN_SRC:-}"
BIN_SRC="${BIN_SRC:-bin}"
need_build=0
for bin in linux-dashboard-server linux-dashboard-helper; do
  [[ -x "${BIN_SRC}/${bin}" ]] || need_build=1
done
# Binary yang ADA belum tentu binary yang BENAR. Menjalankan ulang installer
# untuk memperbarui panel menarik sumber baru lewat `git reset --hard`, tapi
# bin/ masih berisi hasil build lama — dan tanpa pemeriksaan ini skrip
# memasang ulang binary lama itu, lalu melaporkan "Terpasang". Gejalanya:
# perbaikan yang sudah ada di main tidak pernah muncul di mesin, dan tidak ada
# satu pun baris log yang menyebut kenapa.
#
# Yang dibandingkan mtime, bukan commit: `git reset --hard` menyentuh berkas
# yang berubah, jadi sumber yang baru ditarik otomatis lebih baru daripada
# binary lama. `make build` meninggalkan binary lebih baru daripada sumbernya,
# jadi alur `make install` tetap tidak membangun ulang tanpa perlu.
#
# BIN_SRC yang diberikan user sengaja dikecualikan: itu berarti binary-nya
# dibangun di tempat lain, dan sumber di sini tidak menggambarkannya.
if (( ! need_build )) && [[ -z "$BIN_SRC_DIBERIKAN" ]]; then
  if [[ -n "$(find cmd internal web/ui/src go.mod go.sum Makefile \
      -newer "${BIN_SRC}/linux-dashboard-helper" -print -quit 2>/dev/null)" ]]; then
    log "Sumber lebih baru daripada binary di ${BIN_SRC}/ — build ulang"
    need_build=1
  fi
fi
# pastikan_npm menjaga npm tetap versi terbaru sebelum build.
#
# npm mencetak blok "New major version of npm available!" di TENGAH log build
# setiap kali versinya tertinggal. Itu bukan peringatan yang bisa
# ditindaklanjuti user di sini — mereka sedang memasang panel, bukan mengurus
# toolchain Node — dan mudah tertukar dengan pesan error yang sebenarnya.
#
# Dua langkah, karena keduanya menjawab hal berbeda: notifier dimatikan supaya
# log build bersih apa pun hasilnya, dan npm-nya sendiri diperbarui supaya
# yang dipakai memang versi terkini.
pastikan_npm() {
  command -v npm >/dev/null 2>&1 || return 0
  # Berlaku untuk seluruh proses turunan skrip ini, termasuk `npm ci` dan
  # `npm run build` yang dipanggil Makefile.
  export NPM_CONFIG_UPDATE_NOTIFIER=false

  local sekarang terbaru
  sekarang=$(npm --version 2>/dev/null) || return 0
  # Registry bisa tidak terjangkau (mesin offline, di balik proxy). Itu bukan
  # alasan menggagalkan instalasi — build tetap jalan dengan npm yang ada.
  terbaru=$(npm view npm version 2>/dev/null) || {
    log "npm ${sekarang} — pengecekan versi dilewati (registry tidak terjangkau)"
    return 0
  }
  if [[ -z "$terbaru" || "$sekarang" == "$terbaru" ]]; then
    ok "npm ${sekarang} sudah terbaru"
    return 0
  fi
  log "npm ${sekarang} → ${terbaru}, memperbarui…"
  if npm install -g "npm@${terbaru}" >/dev/null 2>&1; then
    ok "npm diperbarui ke $(npm --version)"
  else
    echo "[⚠] Gagal memperbarui npm — build tetap dilanjutkan dengan ${sekarang}." >&2
  fi
}

if (( need_build )); then
  if ! command -v go >/dev/null || ! command -v npm >/dev/null; then
    install_build_deps
  fi
  pastikan_npm
  log "Build UI + dua binary (beberapa menit di mesin kecil)…"
  export GOTOOLCHAIN=auto
  make build
fi

log "Arsitektur: $(uname -m)"

# ---- 3. Pasang service --------------------------------------------------
# Akun service tanpa shell login: proses web tidak pernah perlu sesi interaktif.
if ! id -u "$SERVICE_USER" >/dev/null 2>&1; then
  useradd --system --no-create-home --shell /usr/sbin/nologin "$SERVICE_USER"
  ok "User sistem ${SERVICE_USER} dibuat"
fi

install -m 0755 "${BIN_SRC}/linux-dashboard-server" "${PREFIX}/linux-dashboard-server"
install -m 0755 "${BIN_SRC}/linux-dashboard-helper" "${PREFIX}/linux-dashboard-helper"
# Command CLI `uninstall-linuxpanel` — dipasang sebelum servis dimatikan
# supaya tetap tersedia setelah uninstall (untuk reinstall bila perlu).
install -m 0755 deploy/uninstall-linuxpanel.sh "${PREFIX}/uninstall-linuxpanel"
ok "Binary dipasang di ${PREFIX}"
ok "Command uninstall-linuxpanel terpasang (CLI)"

install -m 0644 deploy/pam.d/linux-dashboard /etc/pam.d/linux-dashboard
install -m 0644 deploy/linux-dashboard-helper.service /etc/systemd/system/
install -m 0644 deploy/linux-dashboard-web.service /etc/systemd/system/
# Unit 9router ditulis kalau belum ada — admin yang sudah mengelola sendiri
# (ExecStart custom, env var khusus) tidak boleh ditimpa setiap Update.
if [[ ! -f /etc/systemd/system/9router.service ]]; then
  install -m 0644 deploy/9router.service /etc/systemd/system/9router.service
  ok "Unit systemd 9router.service dipasang"
fi
# Salin unit ke /usr/local/share/linux-dashboard supaya daemon runtime
# (yang dipasang terpisah dari source tree via Update) bisa menemukan
# unit ketika user memilih "Pasang 9router" dari halaman Components.
install -d -m 0755 /usr/local/share/linux-dashboard
if [[ -f deploy/9router.service ]]; then
  install -m 0644 deploy/9router.service /usr/local/share/linux-dashboard/9router.service
fi
# Skrip uninstaller inti — dipasang di share direktori agar command
# `uninstall-linuxpanel` (di /usr/local/bin) selalu bisa menemukannya,
# tanpa harus bergantung pada source tree yang mungkin sudah dihapus
# oleh mode 'panel'. Berkas ini DUPLIKAT dari internal/helper/uninstall.sh
# yang di-embed ke binary helper — satu sumber, dua representasi, sama
# isinya. Installer menyalin keduanya agar update sistem tidak kehilangan
# command CLI ini.
if [[ -f internal/helper/uninstall.sh ]]; then
  install -m 0644 internal/helper/uninstall.sh /usr/local/share/linux-dashboard/uninstall.sh
  ok "Skrip uninstall.sh dipasang ke share direktori"
fi

# Setelan per-device. Hanya dibuat kalau belum ada — isinya milik pemilik mesin,
# dan menimpanya tiap Update akan mengembalikan port/secure cookie ke default.
if [[ ! -f /etc/default/linux-dashboard ]]; then
  install -m 0644 deploy/linux-dashboard.default /etc/default/linux-dashboard
  ok "Setelan per-device dibuat di /etc/default/linux-dashboard"
else
  ok "Setelan per-device di /etc/default/linux-dashboard dipertahankan"
fi

install -d -o root -g "$SERVICE_USER" -m 0750 /var/lib/linux-dashboard

# ---- TLS native langsung, tanpa reverse proxy ----------------------------
# Sertifikat custom dipertahankan. Jika belum ada, buat self-signed agar panel
# publik tidak pernah lahir sebagai HTTP. Browser akan memberi peringatan sampai
# cert ini dipercaya/import atau diganti cert CA, tetapi transport tetap
# terenkripsi dan cookie selalu Secure.
set_env_dashboard() { # key value
  local key="$1" value="$2" file=/etc/default/linux-dashboard tmp
  tmp=$(mktemp "${file}.XXXXXX")
  awk -v k="$key" '$0 !~ "^[[:space:]]*" k "=" { print }' "$file" > "$tmp"
  printf '%s=%s\n' "$key" "$value" >> "$tmp"
  chmod --reference="$file" "$tmp"
  chown --reference="$file" "$tmp"
  mv -f "$tmp" "$file"
}

ambil_env_dashboard() { # key
  local key="$1"
  { grep -E "^[[:space:]]*${key}=" /etc/default/linux-dashboard 2>/dev/null || true; } |
    cut -d= -f2- | tail -n 1 | tr -d '"'\'' '
}

tls_cert=$(ambil_env_dashboard DASHBOARD_TLS_CERT)
tls_key=$(ambil_env_dashboard DASHBOARD_TLS_KEY)
# DASHBOARD_ALLOW_PLAINTEXT=true berarti operator sengaja menangani TLS di
# luar proses (reverse proxy). Runtime memprioritaskan DASHBOARD_TLS_CERT
# (config.LoadValidated cabang punyaCert menang duluan), jadi kalau cabang
# else di bawah menulis sertifikat, upstream proxy yang tadinya
# http://127.0.0.1:1122 diam-diam berubah jadi https pada update berikutnya
# sampai proxy 502 — dan nilai operator akan ditimpa lagi tiap run. Karena itu
# pilihan plaintext diekspresikan lebih dulu dan TIDAK ditimpa installer.
plaintext_dipilih=false
case "$(ambil_env_dashboard DASHBOARD_ALLOW_PLAINTEXT | tr '[:upper:]' '[:lower:]')" in
  1|t|true) plaintext_dipilih=true ;;
esac
if [[ -n "$tls_cert" || -n "$tls_key" ]]; then
  [[ -n "$tls_cert" && -n "$tls_key" ]] || die "TLS parsial: DASHBOARD_TLS_CERT dan DASHBOARD_TLS_KEY wajib diisi bersama"
  [[ -f "$tls_cert" && -f "$tls_key" ]] || die "Berkas TLS yang dikonfigurasi tidak ditemukan"
  openssl x509 -in "$tls_cert" -noout >/dev/null 2>&1 || die "Sertifikat TLS tidak valid: $tls_cert"
  openssl pkey -in "$tls_key" -noout >/dev/null 2>&1 || die "Private key TLS tidak valid: $tls_key"
  cert_pub=$(openssl x509 -in "$tls_cert" -pubkey -noout | openssl pkey -pubin -outform DER 2>/dev/null | sha256sum | cut -d' ' -f1)
  key_pub=$(openssl pkey -in "$tls_key" -pubout -outform DER 2>/dev/null | sha256sum | cut -d' ' -f1)
  [[ -n "$cert_pub" && "$cert_pub" == "$key_pub" ]] || die "Sertifikat dan private key TLS tidak berpasangan"
  ok "Sertifikat TLS yang sudah dikonfigurasi dipertahankan"
  if [[ "$plaintext_dipilih" == true ]]; then
    log "Catatan: DASHBOARD_TLS_CERT terisi, jadi TLS native menang; DASHBOARD_ALLOW_PLAINTEXT diabaikan runtime. Hapus DASHBOARD_TLS_CERT/KEY kalau memang ingin terminasi TLS eksternal."
  fi
  set_env_dashboard DASHBOARD_SECURE_COOKIE true
elif [[ "$plaintext_dipilih" == true ]]; then
  ok "DASHBOARD_ALLOW_PLAINTEXT=true — terminasi TLS eksternal dipertahankan; sertifikat native tidak dibuat/ditimpa"
  # Mode ini khusus terminasi TLS eksternal: browser tetap membuka HTTPS,
  # sehingga cookie wajib Secure. Akses HTTP langsung memang tidak dapat login;
  # itu mencegah password/session terkirim plaintext karena salah konfigurasi.
  set_env_dashboard DASHBOARD_SECURE_COOKIE true
else
  tls_dir=/etc/linux-dashboard
  tls_cert="${tls_dir}/tls.crt"
  tls_key="${tls_dir}/tls.key"
  install -d -o root -g "$SERVICE_USER" -m 0750 "$tls_dir"
  if [[ ! -s "$tls_cert" || ! -s "$tls_key" ]]; then
    host=$(hostname 2>/dev/null || echo linux-dashboard)
    san="DNS:${host},DNS:localhost,IP:127.0.0.1,IP:::1"
    while read -r addr; do
      [[ -n "$addr" ]] && san+=",IP:${addr}"
    done < <(hostname -I 2>/dev/null | tr ' ' '\n' | sed '/^$/d' | sort -u)
    umask 077
    openssl req -x509 -newkey rsa:3072 -nodes -sha256 -days 825 \
      -subj "/CN=${host}" -addext "subjectAltName=${san}" \
      -keyout "$tls_key" -out "$tls_cert" >/dev/null 2>&1 || die "Gagal membuat sertifikat TLS"
  fi
  chown root:"$SERVICE_USER" "$tls_key" "$tls_cert"
  chmod 0640 "$tls_key"
  chmod 0644 "$tls_cert"
  set_env_dashboard DASHBOARD_TLS_CERT "$tls_cert"
  set_env_dashboard DASHBOARD_TLS_KEY "$tls_key"
  set_env_dashboard DASHBOARD_SECURE_COOKIE true
  ok "Sertifikat TLS self-signed dibuat/dipakai di ${tls_dir}"
fi

# Key enkripsi TOTP berbeda dari secret HMAC helper. Dibuat sekali, root-owned,
# hanya dapat dibaca grup service web; tidak pernah ditaruh di DB atau log.
totp_key=/var/lib/linux-dashboard-helper/totp.key
install -d -o root -g "$SERVICE_USER" -m 0750 "$(dirname "$totp_key")"
if [[ ! -e "$totp_key" ]]; then
  umask 077
  openssl rand 32 > "$totp_key" || die "Gagal membuat key enkripsi TOTP"
fi
[[ -f "$totp_key" && ! -L "$totp_key" ]] || die "Key TOTP harus berkas regular, bukan symlink"
[[ "$(wc -c < "$totp_key")" -eq 32 ]] || die "Key TOTP harus tepat 32 byte"
chown root:"$SERVICE_USER" "$totp_key"
chmod 0640 "$totp_key"
set_env_dashboard DASHBOARD_TOTP_KEY "$totp_key"

# Folder data per akun: ~/DATA/{AppData,Documents,Downloads,Gallery,Media}.
# Panel juga memastikannya ada tiap File Manager dibuka — yang di sini supaya
# sudah siap sejak install pertama, tanpa menunggu user login. Dibuat milik
# akun masing-masing; folder yang SUDAH ada tidak diubah izinnya sama sekali.
# Akun service dilewati: shell nologin/false berarti akun itu tidak pernah
# login ke panel (aturan yang sama dipakai helper daemon).
# /etc/skel dipakai `useradd -m` (termasuk pembuatan user dari panel) sebagai
# isi awal home baru, dan salinannya otomatis jadi milik user itu. Menaruh
# kerangka DATA di sini membuat akun yang dibuat SETELAH instalasi langsung
# punya folder datanya, tanpa menunggu ia membuka File Manager — penting untuk
# akses yang tidak lewat panel, mis. share Samba `/home/%U/DATA/Documents`.
for dir in AppData Documents Downloads Gallery Media; do
  [[ -d "/etc/skel/DATA/${dir}" ]] || install -d -m 0755 "/etc/skel/DATA/${dir}"
done

dibuat=0
while IFS=: read -r _nama _sandi uid gid _gecos home shell; do
  if [[ "$uid" -ne 0 ]] && { [[ "$uid" -lt 1000 ]] || [[ "$uid" -ge 60000 ]]; }; then
    continue
  fi
  case "$shell" in ""|*/nologin|*/false|*/sync) continue ;; esac
  [[ -d "$home" ]] || continue
  # Induknya dibuat terpisah: `install -d` hanya menerapkan -o/-g/-m ke
  # komponen TERAKHIR, jadi tanpa baris ini ~/DATA terbentuk sebagai root:root
  # dan user tidak bisa membuat folder baru langsung di dalamnya.
  if [[ ! -d "${home}/DATA" ]]; then
    install -d -o "$uid" -g "$gid" -m 0755 "${home}/DATA"
  fi
  for dir in AppData Documents Downloads Gallery Media; do
    target="${home}/DATA/${dir}"
    if [[ ! -d "$target" ]]; then
      install -d -o "$uid" -g "$gid" -m 0755 "$target"
      dibuat=$((dibuat + 1))
    fi
  done
  # Pasang password awal untuk akun yang belum punya. Kolom kedua dari
  # /etc/shadow adalah hash; "!" / "*" artinya akun terkunci (login via
  # password dimatikan). WSL image yang di-import dari cloud biasanya
  # membuat user dengan shell bash tapi password terkunci — login ke panel
  # selalu gagal sampai owner mengetik `passwd oxidilily` di shell.
  if [[ -n "$INITIAL_PASSWORD" ]]; then
    sandinya=$(getent shadow "$_nama" | cut -d: -f2)
    if [[ "$sandinya" == "!" || "$sandinya" == "*" || -z "$sandinya" ]]; then
      # chpasswd membaca "user:pass" dari stdin — tidak lewat argumen command,
      # supaya password tidak muncul di /proc/<pid>/cmdline atau ps output.
      printf '%s:%s\n' "$_nama" "$INITIAL_PASSWORD" | chpasswd
      log "Password awal dipasang untuk akun $_nama — ganti segera setelah login pertama"
    fi
  fi
done < <(getent passwd)
if (( dibuat > 0 )); then
  ok "${dibuat} folder data akun dibuat (~/DATA/*)"
fi

systemctl daemon-reload
systemctl enable linux-dashboard-helper.service linux-dashboard-web.service >/dev/null
# restart, bukan `enable --now`: pada upgrade service lama harus memakai
# binary baru, dan `--now` tidak me-restart unit yang sudah jalan.
systemctl restart linux-dashboard-helper.service
systemctl restart linux-dashboard-web.service

for unit in linux-dashboard-helper linux-dashboard-web; do
  systemctl is-active --quiet "$unit" || die "${unit}.service gagal start — cek: journalctl -u ${unit} -n 50"
done

# ---- 3a. Firewall dan fail2ban wajib aktif -------------------------------
# Tidak pernah reset/delete/default: seluruh rule admin yang sudah ada tetap
# dipertahankan. Rule akses admin dibuat SEBELUM enable agar koneksi SSH tidak
# terkunci di tengah instalasi.
log "Mengamankan akses dengan ufw…"
ssh_ports=()
if command -v sshd >/dev/null 2>&1; then
  while read -r p; do
    [[ "$p" =~ ^[0-9]+$ ]] && ssh_ports+=("$p")
  done < <(sshd -T 2>/dev/null | awk 'tolower($1)=="port" {print $2}' | sort -nu)
fi
if (( ${#ssh_ports[@]} == 0 )); then
  if [[ -r /etc/ssh/sshd_config ]]; then
    while read -r p; do
      [[ -n "$p" ]] && ssh_ports+=("$p")
    done < <(awk 'tolower($1)=="port" && $2 ~ /^[0-9]+$/ {print $2}' /etc/ssh/sshd_config 2>/dev/null)
  fi
  for conf in /etc/ssh/sshd_config.d/*.conf; do
    [[ -r "$conf" ]] || continue
    while read -r p; do
      [[ -n "$p" ]] && ssh_ports+=("$p")
    done < <(awk 'tolower($1)=="port" && $2 ~ /^[0-9]+$/ {print $2}' "$conf" 2>/dev/null)
  done
fi
(( ${#ssh_ports[@]} > 0 )) || ssh_ports=(22)
mapfile -t ssh_ports < <(printf '%s\n' "${ssh_ports[@]}" | sort -nu)
for sp in "${ssh_ports[@]}"; do
  ufw allow "${sp}/tcp" comment 'SSH' >/dev/null 2>&1 || \
    ufw allow "${sp}/tcp" >/dev/null 2>&1 || die "Gagal membuka port SSH ${sp}/tcp di ufw"
done
panel_port=$(ambil_env_dashboard DASHBOARD_LISTEN | awk -F: '{print $NF}')
[[ "$panel_port" =~ ^[0-9]+$ ]] || panel_port=1122
ufw allow "${panel_port}/tcp" comment 'panel linux-dashboard' >/dev/null 2>&1 || \
  ufw allow "${panel_port}/tcp" >/dev/null 2>&1 || die "Gagal membuka port panel ${panel_port}/tcp di ufw"
# Deteksi firewall lain SEBELUM mengaktifkan: firewalld yang sudah aktif berarti
# dua manajer firewall akan berebut aturan yang sama. Panel tidak menonaktifkan
# apa pun yang sudah dipasang — itu urusan admin.
if command -v firewall-cmd >/dev/null 2>&1 && firewall-cmd --state >/dev/null 2>&1; then
  echo "[⚠] firewalld terdeteksi AKTIF. UFW tetap diaktifkan sesuai permintaan instalasi," >&2
  echo "     tapi dua firewall aktif bisa saling menutupi — nonaktifkan salah satu kalau" >&2
  echo "     koneksi panel/SSH bermasalah setelah ini." >&2
fi

# Port non-loopback yang sedang listen tanpa rule: begitu default deny aktif,
# akses dari luar ke sana ikut tertutup. Peringatan saja — supaya admin tahu
# sebelum heran layanannya hilang dari luar, bukan alasan membatalkan install.
if command -v ss >/dev/null 2>&1; then
  izin=$(ufw status 2>/dev/null | awk '/ALLOW/ {print $1}' || true)
  tanpa_rule=()
  while read -r lp; do
    [[ -n "$lp" ]] || continue
    [[ "$lp" == "$panel_port" ]] && continue
    printf '%s\n' "${ssh_ports[@]}" | grep -qx "$lp" && continue
    printf '%s\n' "$izin" | grep -qE "^${lp}/(tcp|udp)$" && continue
    tanpa_rule+=("$lp")
  done < <(ss -H -lnt 2>/dev/null | awk '{print $4}' | grep -vE '^(127\.|\[::1\]|::1):' | sed 's/.*://' | sort -un)
  if (( ${#tanpa_rule[@]} > 0 )); then
    echo "[⚠] Port listen non-loopback tanpa rule UFW: ${tanpa_rule[*]}" >&2
    echo "     Akses luar ke port itu tertutup setelah enable. Tambahkan 'ufw allow" >&2
    echo "     <port>/tcp' lebih dulu kalau memang perlu diakses dari luar." >&2
  fi
fi

# Enable hanya kalau firewall belum aktif — state dan rule yang sudah berjalan
# tidak disentuh (permintaan: tanpa reset/delete/default pada rule yang ada).
if ufw status 2>/dev/null | grep -qi '^Status: active'; then
  ok "UFW sudah aktif; rule lama dipertahankan, SSH (${ssh_ports[*]}/tcp) dan panel (${panel_port}/tcp) diizinkan"
else
  ufw --force enable >/dev/null || die "Gagal mengaktifkan ufw"
  ufw status | grep -qi '^Status: active' || die "ufw tidak aktif setelah enable"
  ok "UFW aktif; rule lama dipertahankan, SSH (${ssh_ports[*]}/tcp) dan panel (${panel_port}/tcp) diizinkan"
fi

log "Mengaktifkan fail2ban…"
# Paket Debian/Ubuntu mengaktifkan service tapi jail sshd-nya default disabled
# di jail.conf. Agar perlindungan brute-force benar-benar ada, tulis drop-in
# kecil berisi HANYA `enabled = true` untuk jail sshd. Berkas ini dibaca
# sebelum jail.local, jadi kalau admin memang sengaja menonaktifkan sshd di
# jail.local, pilihan admin tetap menang dan tidak ada yang ditimpa.
sshd_dropin=/etc/fail2ban/jail.d/linux-dashboard-sshd.conf
if [[ ! -e "$sshd_dropin" ]]; then
  install -d -m 0755 /etc/fail2ban/jail.d
  printf '[sshd]\nenabled = true\n' > "$sshd_dropin"
  chmod 0644 "$sshd_dropin"
fi
fail2ban-client -t >/dev/null 2>&1 || die "Konfigurasi fail2ban tidak valid — periksa jail lokal"
systemctl enable --now fail2ban.service >/dev/null || die "Gagal enable/start fail2ban"
systemctl restart fail2ban.service >/dev/null || die "Gagal reload fail2ban"
systemctl is-enabled --quiet fail2ban.service || die "fail2ban tidak enabled"
systemctl is-active --quiet fail2ban.service || die "fail2ban tidak aktif"
# Service hidup ≠ proteksi ada. Jail sshd harus benar-benar dimuat; kalau tidak,
# brute-force SSH tidak diblokir walau fail2ban "aktif". Kegagalan cek di sini
# TIDAK menggagalkan instalasi: bisa jadi distro memakai nama jail lain, atau
# admin sengaja menonaktifkan — keadaan itu harus terlihat sebagai peringatan,
# bukan alasan membatalkan pemasangan panel yang sudah jalan.
if fail2ban-client status sshd >/dev/null 2>&1; then
  ok "fail2ban aktif dengan jail sshd; rule/jail admin yang sudah ada tidak ditimpa"
else
  echo "[⚠] fail2ban berjalan tetapi jail sshd tidak dimuat — proteksi brute-force SSH belum ada." >&2
  echo "     Cek 'fail2ban-client status' dan /etc/fail2ban/jail.local: nama jail bisa" >&2
  echo "     berbeda antar distro, atau admin memang menonaktifkannya." >&2
fi

# ---- 3b. Perkakas dasar yang dipakai helper daemon ---------------------
# Semuanya bagian dari systemd/util-linux dan ada di instalasi Ubuntu/Debian
# normal. Yang dicek di sini adalah image minimal/container yang memangkasnya:
# tanpa perkakas ini beberapa halaman panel gagal tanpa sebab yang jelas.
missing_base=()
for bin in systemctl ip findmnt mount mountpoint useradd; do
  command -v "$bin" >/dev/null 2>&1 || missing_base+=("$bin")
done
if (( ${#missing_base[@]} > 0 )); then
  echo "[⚠] Perkakas dasar tidak ditemukan: ${missing_base[*]}" >&2
  echo "[⚠] Panel tetap terpasang, tapi fitur yang memakainya akan gagal." >&2
fi

# ---- 3c. Tooling GPU sesuai hardware yang benar-benar ada -------------
# Dashboard membaca utilisasi/VRAM GPU lewat alat vendor: rocm-smi (AMD),
# nvidia-smi (NVIDIA), intel_gpu_top (Intel). Tanpa itu ia jatuh ke sysfs,
# yang di banyak kartu hanya mengekspos suhu — panel menampilkan "—" untuk
# utilisasi dan user tidak punya petunjuk apa yang kurang.
#
# Yang dipasang HANYA yang cocok dengan vendor yang terdeteksi di mesin ini.
# Memasang ketiganya di semua mesin berarti menarik paket vendor yang tidak
# akan pernah dipakai, dan untuk NVIDIA itu bisa menyeret driver kernel.
gpu_vendor() { # cetak: amd / nvidia / intel, satu per baris, unik
  {
    # ID vendor PCI dari kartu DRM — sama dengan yang dipakai panel di
    # internal/metrics/gpu.go (pciVendors).
    for v in /sys/class/drm/card*/device/vendor; do
      [[ -r "$v" ]] || continue
      case "$(cat "$v" 2>/dev/null)" in
        0x1002) echo amd ;;
        0x10de) echo nvidia ;;
        0x8086) echo intel ;;
      esac
    done
    # Modul kernel yang dimuat — menangkap kartu yang tidak muncul di
    # /sys/class/drm (mis. NVIDIA proprietary tanpa node DRM).
    [[ -d /sys/module/amdgpu || -d /sys/module/radeon ]] && echo amd
    [[ -d /sys/module/nvidia ]] && echo nvidia
    [[ -d /sys/module/i915 ]] && echo intel
    # `:` menutup grup dengan status 0. Tanpa ini, baris [[ ]] terakhir yang
    # gagal (vendor itu tidak ada) membuat seluruh grup bernilai bukan-nol,
    # dan `set -o pipefail` di atas menularkannya ke pipeline — sehingga
    # `vendors=$(gpu_vendor)` dianggap gagal dan `set -e` menghentikan
    # installer di tengah jalan, tepat setelah panel selesai dipasang.
    :
  } 2>/dev/null | sort -u
}

# apt_ada memastikan paketnya benar-benar punya kandidat sebelum dipasang —
# `apt-get install` untuk nama yang tidak dikenal repo akan gagal, dan skrip
# ini berjalan di bawah `set -e`.
apt_ada() {
  [[ -n "$(apt-cache policy "$1" 2>/dev/null | awk '/Candidate:/ && $2 != "(none)" {print $2}')" ]]
}

# perangkat_gpu_ada memeriksa node device yang benar-benar dibutuhkan alat
# vendor, bukan cukup keberadaan kartunya di sysfs.
#
# Ini bukan kehati-hatian teoretis: container LXC ikut melihat /sys milik
# kernel host, jadi /sys/class/drm/card0 dan /sys/module/amdgpu tampak ada
# lengkap dengan ID vendornya — padahal tanpa passthrough /dev/dri, rocm-smi
# yang dipasang di dalamnya tidak akan pernah menemukan satu pun GPU. Yang
# menentukan bisa-tidaknya adalah node device, bukan sysfs.
perangkat_gpu_ada() { # vendor
  case "$1" in
    amd)    [[ -e /dev/kfd || -d /dev/dri ]] ;;
    intel)  [[ -d /dev/dri ]] ;;
    nvidia) [[ -e /dev/nvidiactl || -e /dev/nvidia0 || -d /dev/dri ]] ;;
    *)      return 1 ;;
  esac
}

pasang_gpu_tool() { # paket, binary, vendor
  command -v "$2" >/dev/null 2>&1 && { ok "  $3: $2 sudah ada"; return 0; }
  if ! apt_ada "$1"; then
    echo "[i]   $3: paket $1 tidak tersedia di repo mesin ini — dilewati"
    return 0
  fi
  log "  $3: memasang $1…"
  if apt-get install -y --no-install-recommends "$1" >/dev/null 2>&1; then
    ok "  $3: $2 terpasang"
  else
    echo "[⚠]   $3: pemasangan $1 gagal — dashboard tetap memakai pembacaan sysfs" >&2
  fi
}

vendors=$(gpu_vendor)
if [[ -z "$vendors" ]]; then
  log "GPU tidak terdeteksi di mesin ini — tooling GPU dilewati"
  log "  (normal di VM/LXC/WSL tanpa passthrough; dashboard menjelaskannya sendiri)"
else
  log "GPU terdeteksi: $(echo "$vendors" | tr '\n' ' ')"
  apt-get update -qq >/dev/null 2>&1 || true
  while read -r v; do
    if ! perangkat_gpu_ada "$v"; then
      echo "[i]   ${v}: kartu terlihat di sysfs tapi node device-nya tidak ada — dilewati"
      echo "[i]   (khas container/VM tanpa passthrough; alat vendor tidak akan menemukan GPU)"
      continue
    fi
    case "$v" in
      amd)   pasang_gpu_tool rocm-smi rocm-smi AMD ;;
      intel) pasang_gpu_tool intel-gpu-tools intel_gpu_top Intel ;;
      nvidia)
        # nvidia-smi ikut driver proprietary, jadi biasanya sudah ada kalau
        # modulnya dimuat. Debian punya paket bernama persis "nvidia-smi";
        # Ubuntu menyediakannya lewat nvidia-utils-<branch>, dan branch-nya
        # TIDAK boleh ditebak: memasang branch yang tidak cocok dengan modul
        # yang sedang jalan membuat nvidia-smi menolak bicara dengan driver.
        # Karena itu branch diambil dari versi modul yang benar-benar dimuat.
        if command -v nvidia-smi >/dev/null 2>&1; then
          ok "  NVIDIA: nvidia-smi sudah ada"
        elif apt_ada nvidia-smi; then
          pasang_gpu_tool nvidia-smi nvidia-smi NVIDIA
        elif [[ -r /sys/module/nvidia/version ]]; then
          branch=$(cut -d. -f1 < /sys/module/nvidia/version)
          if apt_ada "nvidia-utils-${branch}"; then
            pasang_gpu_tool "nvidia-utils-${branch}" nvidia-smi NVIDIA
          else
            echo "[i]   NVIDIA: nvidia-utils-${branch} tidak ada di repo — pasang sendiri sesuai driver terpasang"
          fi
        else
          # Tidak ada modul yang dimuat = tidak ada versi yang bisa dicocokkan.
          # Menebak branch di sini berarti menarik driver kernel ke mesin
          # orang lain atas dasar tebakan; itu keputusan pemilik mesin.
          echo "[i]   NVIDIA: driver proprietary belum dimuat — pasang driver NVIDIA dulu, nvidia-smi ikut di dalamnya"
        fi
        ;;
    esac
  done <<< "$vendors"
fi

log "Komponen opsional yang terdeteksi:"
cek_komponen() { # nama, binary, halaman
  if command -v "$2" >/dev/null 2>&1 || [[ -x "/usr/sbin/$2" ]]; then
    ok "  ${1} sudah ada — ${3} siap dipakai"
  else
    echo "[i]   ${1} belum ada — ${3} akan tampil \"Belum Terpasang\" sampai dipasang di menu Components"
  fi
}
cek_komponen samba       smbd            "File manager → Samba"
cek_komponen mergerfs    mergerfs        "File manager → Disk Pool"
cek_komponen nfs-server  exportfs        "File manager → NFS Exports"
cek_komponen ufw         ufw             "Settings → Firewall"
cek_komponen fail2ban    fail2ban-client "Settings → Fail2ban"
cek_komponen docker      docker          "System → Docker"
cek_komponen nodejs      node            "Components → 9Router"
cek_komponen tailscale   tailscale       "Settings → Network (Tailscale)"
cek_komponen cloudflared cloudflared     "Settings → Network (Cloudflare Tunnel)"
cek_komponen stalwart    stalwart        "Components → Stalwart (server email)"

ip=$(hostname -I 2>/dev/null | awk '{print $1}')
if [[ "$plaintext_dipilih" == true && -z "$tls_cert" && -z "$tls_key" ]]; then
  ok "Terpasang. Buka URL HTTPS milik reverse proxy/terminator TLS Anda."
  echo "[i] Backend panel mendengar HTTP pada ${ip:-<ip-server>}:${panel_port}; jangan buka port ini langsung ke internet."
  echo "[i] Cookie sesi tetap Secure dan hanya dikirim browser melalui HTTPS."
else
  ok "Terpasang. Buka https://${ip:-<ip-server>}:${panel_port}"
  echo "[i] TLS native aktif; cookie sesi Secure. Sertifikat self-signed perlu dipercaya"
  echo "[i] di perangkat Anda atau dapat diganti lewat Settings → Certificates."
fi
echo "[i] Login pakai akun Linux yang sudah ada di mesin ini."
echo "[i] UFW dan fail2ban aktif; rule UFW yang sudah ada tidak di-reset/dihapus."
