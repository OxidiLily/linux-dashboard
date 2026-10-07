**Bahasa Indonesia** · [English](README.en.md)

# Linux Server Dashboard (Go + React)

Panel web untuk memonitor & mengelola satu server Linux: metrik real-time,
file manager, berbagi file (Samba/NFS/mergerfs), print server (CUPS), proses,
Docker, terminal, firewall, dan pengaturan sistem. Login memakai akun Linux
yang sudah ada — tidak ada tabel user terpisah.

Target: **Ubuntu & Debian**, amd64/arm64/armhf, ringan di mesin **2 core**.

> **Disclaimer:** masih tahap pengembangan, mohon maaf kalau ada bug.

## Instalasi cepat

```bash
cd / && curl -fsSL https://raw.githubusercontent.com/OxidiLily/linux-dashboard/main/deploy/install.sh | sudo bash
```

- Memasang dependency build + keamanan (Go, Node 24, `libpam0g-dev`,
  `openssl`, `acl`, `ufw`, `fail2ban`), build UI + dua binary, memasang unit
  systemd + file PAM, menyalakan HTTPS native port **1122** (sertifikat
  self-signed bila belum ada).
- **Deteksi dulu, baru memasang**: dependency yang sudah ada dilewati; Go
  dari tarball/asdf/snap tidak diganti paket `golang-go`.
- UFW/fail2ban diaktifkan tanpa reset atau menghapus rule yang sudah ada.
- Perintah sama lagi = upgrade ke `main` terbaru.
- Butuh akun bergrup `sudo` untuk menu Docker, Firewall, Fail2ban, Samba,
  Disk Pool, NFS, dan Components.

## Menu

| Grup | Menu |
|---|---|
| Home | Dashboard (CPU, RAM, Storage, GPU, Network real-time; disk kosong bisa diformat & di-mount, mount bisa dilepas) |
| File manager | File Manager (editor, buat file, cetak, unggah berprogres, pencarian nama sampai subfolder) · Samba · Disk Pool (mergerfs) · NFS Exports · Bookmarks |
| AI | AI Agent (sesi CLI agent di panel: claude-code, codex, opencode, hermes, openclaw) |
| Logs | Logs (alert) · File Operations · Activity Logs |
| Settings | Network (DNS + Tailscale/Cloudflare Tunnel) · Firewall (ufw) · Fail2ban · Alert Thresholds · Print server (CUPS) · Proxy manager (nginx + certbot) · Components |
| System | Processes · Docker · Cronjob · Terminal |

Halaman **Pembaruan** (`/updates`) tidak punya entri sidebar — pintu masuknya
ikon notifikasi di topbar.

- **Akun** tidak di sidebar: pintu masuknya blok profil di kaki sidebar
  (identitas, Akun, Uninstall panel, Keluar). Rute tetap `/settings/account`.
- Halaman yang butuh software tertentu menampilkan **"Belum Terpasang"** +
  tombol ke Components, bukan `command not found`.

### Perilaku inti

- **Pencarian File Manager** = `grep -r` pada NAMA berkas: ketik → Enter/Cari,
  menembus subfolder folder terbuka. Isi berkas tidak dibaca (data bisa
  puluhan GB). Sambil mengetik, daftar disaring di klien (nol request); begitu
  query berubah, hasil lama tidak tertinggal. Karakter `.`/`*` literal. Hasil
  dibatasi 500 baris / 20 detik, dinyatakan di layar; `/proc`, `/sys`, device
  dir tidak ditelusuri.
- **Cronjob** hanya mengedit crontab akun yang login (`crontab -l`/`-` via
  helper), tanpa sudo. Server menjaga isi maks 64 KiB, field `previous` wajib
  (mismatch → HTTP 409), dan pembacaan ulang setelah menulis. Halaman juga
  melaporkan apakah unit cron benar-benar berjalan.
- **Logs** tiga sudut, umur simpan ditegakkan server: Logs (alert, 1 bulan),
  File Operations (1 bulan), Activity Logs (audit login & aksi admin, 2
  tahun), disapu saat start + tiap jam.
- **Pekerjaan panjang tidak batal pindah halaman**: sesi Terminal & AI Agent
  disimpan di luar daur hidup React (F5 tetap menutup); toast aksi helper
  dipasang di app-shell sehingga ikut berpindah halaman; selesai → daftar
  reload sendiri, dan halaman Components mengangkat aksi yang masih jalan
  (`/api/components/progress`).
- **Mobile**: sidebar jadi drawer di bawah `lg`, tabel jadi tumpukan kartu di
  bawah 640px lewat satu kelas CSS + `data-label` (struktur `<table>` tetap
  satu sumber). Rename/Edit/Ubah Permission punya tombol per-baris.
- **i18n** Indonesia + Inggris penuh (termasuk pesan error backend); pilihan
  bahasa & zona waktu disimpan per akun di server.

## Components

36 software opsional (daftar resmi: `ComponentNames()` di
`internal/helper/components.go`), dipasang/dicopot dari panel:

| Kategori | Isi |
|---|---|
| Runtime & tunnel | nginx · certbot · docker · nodejs · tailscale · cloudflared |
| AI & Agent | 9router · hermes · claude-code · codex · opencode · openclaw · rtk · graphify · ponytail · browser-use |
| Database & backend | supabase |
| Berbagi file & jaringan | samba · nfs-server · nfs-client · cifs-utils · avahi · technitium-dns · print-server · mergerfs |
| Email & kolaborasi | mailcow |
| Keamanan | ufw · fail2ban |
| Monitoring & disk | lm-sensors · smartmontools · nvme-cli · qemu-guest-agent |
| Utilitas | htop · ncdu · fastfetch · restic |

- Software yang sudah ada dikenali apa adanya, **tidak dipasang ulang**.
- Bar berpersen datang dari `APT::Status-Fd` (indeks 0–10%, unduh 10–55%,
  pasang 55–99%, tidak pernah turun); skrip vendor ikut terbaca via
  `APT_CONFIG`. Badge "Sedang dipasang" selama bar jalan.
- **Copot** menawarkan "hapus data juga" hanya untuk komponen ber-flag
  `has_data`; default mati.
- `cloudflared` satu-satunya tanpa tombol Jalankan/Hentikan (kendali di
  Settings → Network); mencopotnya ikut membuang `cloudflared.service` yang
  memuat token tunnel.
- **Uninstall panel** 4 mode bertingkat: `panel` → `panel-data` (+ database,
  kunci sesi, `/etc/default`, akun service) → `total` (+ seluruh components)
  → `total-data` (+ folder `~/DATA` tiap akun & `/etc/skel/DATA`, wajib ketik
  `HAPUS DATA`). Akun Linux & home directory tidak pernah dihapus.

### Supabase self-hosted

- Menjalankan **setup.sh resmi** Supabase ke `/opt/supabase/supabase-project`
  (sparse-clone tag rilis + generate semua rahasia). Panel tidak menyusun
  compose/kunci sendiri.
- Panel: memasang Docker lewat jalur panel (akun ikut grup `docker`),
  mengarahkan `SUPABASE_PUBLIC_URL`/`API_EXTERNAL_URL` ke IP LAN, menjalankan
  `sh run.sh start --wait-timeout 600`, lalu **mendaftarkan stack sendiri**
  ke System → Docker (idempoten, dikunci path compose).
- Nama stack **wajib** `supabase`. Hanya port **8000** yang didaftarkan ke
  ufw (5432/6543 urusan admin di Settings → Firewall). Tombol **Buka** →
  basic auth gateway; kredensial di `/opt/supabase/supabase-project/.env`
  (DASHBOARD_USERNAME/PASSWORD), tidak pernah dicetak di kartu.
- Folder proyek diserahkan ke user panel **kecuali `volumes/`** (milik
  container). Berlaku juga untuk `.env`/compose stack lain saat disimpan dari
  panel (hanya file milik root; direktori sistem tidak pernah ikut).
- **Simpan `.env` ≠ terapkan**: container baca env saat *dibuat*, jadi panel
  menawarkan **Up** setelah simpan — Restart tidak cukup.
- Down/Restart mati pada stack tanpa container (dibedakan via
  `docker compose ps -a`).
- **Copot tidak menghapus data**: folder dipindah ke
  `/opt/supabase/bekas-<tanggal>-<jam>`; "hapus data juga" membuang
  `/opt/supabase` seluruhnya.

### mailcow (server email)

- Stack Docker Compose resmi: https://docs.mailcow.email/getstarted/install/.
- Hostname email FQDN wajib; DNS A/AAAA, MX, PTR, SPF, DKIM, DMARC
  dikonfigurasi sesuai https://docs.mailcow.email/getstarted/prerequisite-dns/.
- Minimum 6 GiB RAM + 1 GiB swap, disk 20 GiB; amd64/arm64.
  LXC/OpenVZ tidak didukung upstream; gunakan mesin fisik atau VM penuh.
- Membutuhkan Docker >= 24 dan Compose >= 2.18. Konfigurasi/data berada di
  `/opt/mailcow-dockerized` serta volume Docker; bukan daemon email host.
- Instalasi baru bind HTTP `0.0.0.0:8080` dan HTTPS `0.0.0.0:8443`, tidak
  mengambil port 80/443 milik Proxy manager. Buka `https://<IP-server>:8443/admin/`
  dari perangkat lain; `0.0.0.0` adalah binding, bukan alamat browser. HTTP tidak
  terenkripsi; HTTPS awal self-signed. Akun admin bawaan dapat dijangkau jaringan:
  batasi akses ke perangkat tepercaya, segera ganti password dan aktifkan 2FA
  sebelum membuka internet. Binding instalasi existing tidak diubah. Installer
  menolak perubahan daemon/restart Docker; siapkan IPv6 manual bila diperlukan.
  Sertifikat email tetap harus disiapkan
  untuk SMTP/IMAP, bukan hanya sertifikat reverse proxy web.
- Docker published ports tidak dibatasi oleh rule INPUT UFW. Jangan
  menonaktifkan firewall otomatis; periksa FORWARD/DOCKER-USER dan firewall luar.
- Stack otomatis terdaftar di System → Docker; project `mailcowdockerized`
  tetap sama setelah Down/Up. Editor konfigurasi generik dinonaktifkan untuk
  menjaga root ownership dan symlink `.env`; gunakan
  `sudoedit /opt/mailcow-dockerized/mailcow.conf`, pertahankan mode 0600.
- Penggantian komponen panel tidak memigrasikan mailbox atau menghapus
  instalasi email lama. Cadangkan email sebelum migrasi terpisah.

### Alat wajib AI Agent

Empat komponen AI terakhir bukan agent, melainkan alat untuk **semua** agent
— dipasang otomatis begitu agent dipasang, dan arahannya ditulis ke berkas
instruksi global tiap sesi AI Agent dibuka:

| Alat | Peran |
|---|---|
| rtk | Memangkas keluaran shell sebelum masuk konteks agent |
| graphify | Knowledge graph kode via parsing AST lokal |
| ponytail | Harness "lazy senior dev" + skill audit/review/debt |
| browser-use | Kendali browser via CDP (halaman JS, login, klik, form) |

Pendaftaran **per user & per agent**, tepat sebelum sesi dibuka (installer
hanya menambal `/root`):

| Agent | rtk | graphify | browser-use |
|---|---|---|---|
| claude-code | `rtk init -g --auto-patch --no-trust-filters` | `graphify install --platform claude` | `--target claude` |
| codex | `rtk init -g --codex` | `--platform codex` | `--target codex` |
| opencode | `rtk init -g --opencode --auto-patch --no-trust-filters` | `--platform opencode` | `--target opencode` |
| hermes | `rtk init -g --agent hermes` | `--platform hermes` | — |
| openclaw | — (belum ada target) | `--platform claw` | `--target openclaw` |

`--auto-patch` + `--no-trust-filters` wajib untuk target yang menambal
`settings.json` (daemon tidak punya terminal untuk menjawab prompt).
`browser-use skill install --no-install --target …` juga wajib `--no-install`
agar tidak memasang salinan kedua via `uv`.

### Provider inferensi lewat 9router

Sesi pertama **hermes** dan **openclaw** berhenti menunggu provider; panel
menyambungkannya otomatis ke 9router (`:20128`, gateway OpenAI-compatible):

| Agent | Disambungkan | Cara |
|---|---|---|
| hermes | otomatis | `~/.hermes/config.yaml` + `OPENAI_API_KEY` di `~/.hermes/.env` |
| openclaw | otomatis | `openclaw onboard --non-interactive --auth-choice custom-api-key --custom-provider-id 9router` |
| claude-code · codex · opencode | tidak — opsional, manual | config masing-masing |

Claude Code/Codex/OpenCode sengaja tidak dipaksa (mereka punya login
sendiri). API key dibaca dari tabel `apiKeys` di `~/.9router/db/data.sqlite`,
tidak pernah ditebak. Config yang sudah menyebut provider tidak pernah
ditimpa. Contoh config manual (blok `env` Claude Code, `provider.9router`
OpenCode — keduanya `baseURL http://127.0.0.1:20128/v1`) ada di git history
README ini.

## Docker — sumber daya & pemakaian disk

Tab Images/Volume/Network + **ringkasan pemakaian disk** dari
`docker system df` (tanpa `-v`):

| Baris | Bersihkan | Perintah |
|---|---|---|
| Images | ada | `docker image prune -f -a` |
| Containers | — | (hapus satu per satu di panel) |
| Local Volumes | ada | `docker volume prune -f` |
| Build Cache | ada | `docker builder prune -f` |

- **Dua tingkat prune image disengaja**: tombol tab Images = prune polos
  (dangling saja); tombol baris ringkasan = `prune -a` (termasuk image stack
  yang sedang Down) — keduanya punya konfirmasi sendiri.
- Hapus per item **tidak pernah pakai `-f`** (penolakan daemon = pengaman);
  network bawaan (`bridge`/`host`/`none`) tanpa tombol hapus.
- Whitelist helper disusun per sumber daya (`system` hanya `df`, `builder`
  hanya `prune`); `docker system prune` tidak pernah tersedia.

## Print server (CUPS)

Komponen **opsional** (di LXC CUPS memang tidak bisa jalan), alur cetak
selesai di panel:

1. Components → print-server: pasang `cups` + `printer-driver-gutenprint`
   (wajib — printer USB rumahan umumnya tanpa IPP Everywhere).
2. Settings → Print server → **Deteksi**: `lpinfo` + driver tersedia +
   antrean, agar "siap didaftarkan" bisa dibedakan dari "driver belum ada".
3. **Pasang driver**: frontend mengirim nama **vendor**, mapping vendor →
   paket adalah whitelist backend (`internal/helper/printer.go`).
4. **Daftarkan antrean**, lalu cetak dari File Manager (printer, jumlah,
   media, satu/dua sisi) dan pantau antrean.

Ubah daftar printer/scan/driver = sudo; melihat & mencetak berkas sendiri
tidak. File dialirkan lewat worker yang sudah turun privilege ke stdin `lp`;
skema `file://` ditolak. Deteksi jaringan butuh Avahi (sengaja bukan
dependensi print-server).

## Proxy manager (nginx + certbot)

Halaman `Settings → Proxy manager` (dijaga `ComponentGuard nginx`; TLS
butuh `certbot`). Tiga tab:

- **Proxy Manager** — daftar proxy host: domain/IPv4 → target upstream
  (IP/hostname + port + scheme http/https), aktif/nonaktif, **Uji config**
  (`nginx -t`) dan **Muat ulang**. Ada host bawaan "Panel bawaan" yang tidak
  bisa dihapus (target/port/scheme tetap bisa diubah).
- **SSL/TLS** — terbitkan sertifikat per host: certbot **HTTP-01** (domain
  harus mengarah ke server ini, port 80 terbuka dari internet) atau
  **DNS-01 Cloudflare** memakai token tersimpan, plus mode staging untuk
  pengujian. Matikan TLS menampilkan peringatan bahwa akses berikutnya
  (password, OTP, sesi) jadi tidak terenkripsi.
- **DNS Cloudflare** — simpan/hapus token API (hanya ditampilkan tersamar),
  muat zone + record, tambah/edit/hapus record, dan aksi massal pada record
  terpilih: nyalakan proxy, matikan proxy (dns-only), atau hapus.

## Firewall

`ufw` dipasang `DEFAULT_INPUT_POLICY=DROP`, jadi port **dideklarasikan per
komponen** dan didaftarkan saat komponen dipasang:

| Komponen | Port |
|---|---|
| nginx | 80/tcp · 443/tcp |
| samba | 445/tcp · 139/tcp · 137:138/udp |
| nfs-server | 2049/tcp · 111/tcp · 111/udp |
| avahi | 5353/udp |
| technitium-dns | 53/tcp · 53/udp · 5380/tcp |
| print-server | 631/tcp |
| 9router | 20128/tcp |
| supabase | 8000/tcp · 5432/tcp · 6543/tcp |
| mailcow | 25/tcp · 465/tcp · 587/tcp · 110/tcp · 143/tcp · 993/tcp · 995/tcp · 4190/tcp · 8443/tcp |
| tailscale | 41641/udp |

- Setiap rule panel membawa **label pemilik** (`ufw allow 445/tcp comment
  'Samba'`); memasang ufw belakangan menyusul seluruh port komponen yang
  sudah ada; mencopot komponen mencabut izinnya.
- fail2ban: jail `sshd` menyala otomatis; filter Samba dipasang panel
  sendiri, dengan blok `map to guest = Never` + `log level` disisipkan di
  **akhir** section `[global]` `smb.conf` (nilai terakhir menang, baris
  admin tidak diedit).
- Rule/jail yang dibuat sesudahnya milik user (tidak dibuat ulang). Yang
  dipastikan sebelum firewall menyala hanya akses admin.
- **Penyelaras tiap 30 detik**: layanan hidup → port dibuka & dilabeli
  (rule lama di-update di tempat); layanan mati/unit hilang → izin dicabut.
  Rule yang dihapus sendiri di Settings → Firewall tidak dibuat ulang; yang
  hilang di luar panel (`ufw reset`) dikembalikan. Gagal dibaca ≠ mati
  (putaran ditunda). Catatan: `/var/lib/linux-dashboard-helper/komponen-ports.json`.
- **Port container Docker** ikut dijaga (docker menulis iptables, bukan ufw):
  container jalan → port host didaftarkan; berhenti → dicabut. Dicetuskan
  juga setelah Start/Stop/Restart/Hapus container & `compose up/down`. Rule
  lama milik panel, port komponen, dan rule buatan admin tidak pernah
  disentuh. Catatan: `/var/lib/linux-dashboard-helper/docker-ports.json`.

## Disk & Disk Pool

- Disk mentah (`unused_disks`) muncul di kartu Storage tapi **tidak** ikut
  total storage (ruang belum bisa dipakai). Klik → dialog format (ext4/xfs/
  btrfs) & mount: helper `mkfs`, tulis `/etc/fstab` via UUID + `nofail`,
  mount. Hanya disk yang diakui `UnusedDisks()`; disk berfilesystem ditolak
  (`disk_has_filesystem`) lalu ditawarkan mount tanpa format; `fstab`
  ditulis atomik dan dikembalikan kalau mount gagal.
- Tiap mount punya **lepas** (umount saja, baris fstab tetap) dan **lepas &
  lupakan** (umount + buang baris fstab tulisan panel + hapus folder kosong).
  Isi disk tidak pernah disentuh. Hanya mount di `/mnt`/`/media`; path harus
  lolos `filepath.Clean`; pool/anggota mergerfs dan mount NFS ditolak (arahan
  ke halamannya). Disk dicabut saat ter-mount → `umount -l` hanya bila device
  hilang dari `/dev`.
- **Disk Pool (mergerfs)**: kebijakan bawaan `category.create=pfrd`
  (sebar bobot sisa ruang); pool bisa dipasang/dilepas tanpa menghapus
  definisi (idempoten, tidak menyentuh fstab); mount point dikunci
  immutable selama direktori telanjang; pool ter-mount muncul sebagai
  pintasan "Disk pool : Nama" di File Manager (sudo-only).

## Konfigurasi milik sistem

Berlaku untuk `smb.conf` include, `/etc/fstab`, `/etc/exports`,
`jail.local`, `printers.conf`:

- baris/section bukan tulisan panel ditandai & read-only;
- penulisan via file sementara + `rename`;
- status dibaca dari sistem (`exportfs -s`, `findmnt`, `fail2ban-client
  status`, `lpstat`), bukan dari isi file.

Pengecualian disengaja: blok `[global]` untuk logging Samba (lihat Firewall)
— disisipkan di akhir section, dicadangkan ke `smb.conf.lindash.bak`, hasil
yang ditolak `testparm` dikembalikan otomatis.

## Arsitektur

```
Browser (React SPA)
    │ HTTPS / WebSocket
    ▼
linux-dashboard-server      ← user non-root (linux-dashboard)
  REST API · WebSocket hub · SPA ter-embed · SQLite
    │ Unix socket + HMAC
    ▼
linux-dashboard-helper      ← root
  PAM auth · file ops (fork+setuid) · systemctl · ufw · samba ·
  useradd · apt · docker · PTY
```

Proses web **tidak pernah** punya akses root: operasi privileged dikirim
sebagai command terstruktur, ditandatangani HMAC, dieksekusi dengan argumen
array — tidak pernah lewat `sh -c`. Operasi yang harus berjalan sebagai user
yang login di-fork dengan `SysProcAttr.Credential`, jadi kernel yang
menegakkan izin.

## Struktur

```
cmd/server            entrypoint web app
cmd/helper            entrypoint helper daemon (+ mode worker)
internal/helperproto  kontrak command antara keduanya
internal/helper       implementasi daemon root
internal/helperclient client HMAC ke daemon
internal/api          REST handler + WebSocket
internal/metrics      collector gopsutil + deteksi GPU multi-vendor
internal/platform     deteksi OS/kernel/platform
internal/store        SQLite: session, log, bookmark, threshold, stack
internal/terminal     kuota + daftar sesi terminal
internal/totp         TOTP/TFA
internal/config       konfigurasi dari environment
web/embed.go          go:embed hasil build React
web/ui                sumber frontend (React TSX + Vite + Tailwind v4)
deploy/               unit systemd, file PAM, installer satu baris
```

## Membangun

Butuh **Go 1.26.6+** (lihat `go.mod`, `GOTOOLCHAIN=auto`), **Node.js 20+**,
`libpam0g-dev` (helper pakai PAM via cgo):

```bash
sudo apt install -y build-essential libpam0g-dev
make build          # build UI → embed → dua binary di bin/
sudo ./deploy/install.sh
```

Tanpa `sudo` pun bisa (skrip re-exec dirinya sendiri, `PREFIX` ikut terbawa);
versi yang dipipe dari `curl` tetap harus `| sudo bash`.

- **Build**: `ca-certificates`, `curl`, `git`, `make`, `build-essential`,
  `libpam0g-dev`, Go 1.26.6+, Node 24 dari NodeSource.
- **Runtime dasar**: `systemctl`, `ip`, `hostnamectl`, `resolvectl`,
  `findmnt`, `mount`/`umount`, `useradd`/`usermod`/`userdel`, `apt-get`,
  `dpkg-query`.
- **Runtime opsional** (kelola dari Components, halaman tampil "Belum
  Terpasang" selama belum ada): nginx, certbot, samba, mergerfs,
  nfs-kernel-server, cups+gutenprint, ufw, fail2ban, docker-ce, tailscale,
  cloudflared, mailcow (Docker Compose), nodejs.
- **Library Go**: `chi/v5`, `coder/websocket`, `creack/pty`,
  `msteinert/pam/v2`, `gopsutil/v4`, `modernc.org/sqlite`.
- **Frontend**: React 18 + Vite 6 + TS 5.7, Tailwind v4, Radix Slot,
  Zustand 5, react-router-dom 6, `@xterm/xterm`, lucide-react. Dialog/toast
  adalah source TSX proyek di `src/components/ui/`.

`make release-server` mem-build web app untuk amd64 + arm64 + armhf
sekaligus (cross-compile bawaan Go, `CGO_ENABLED=0`); helper wajib cgo.

## Development

```bash
sudo go run ./cmd/helper       # terminal 1
go run ./cmd/server            # terminal 2
cd web/ui && npm run dev       # terminal 3 → http://localhost:5173
```

Vite mem-proxy `/api` dan `/ws` ke `127.0.0.1:1122`.

## Konfigurasi

Semua lewat environment variable; nilai di bawah adalah default.

| Variabel | Default | Keterangan |
|---|---|---|
| `DASHBOARD_LISTEN` | `127.0.0.1:8080` | Bind web app; installer menyetel `0.0.0.0:1122` + TLS |
| `DASHBOARD_TLS_CERT` | kosong | Sertifikat TLS |
| `DASHBOARD_TLS_KEY` | kosong | Private key TLS |
| `DASHBOARD_ALLOW_PLAINTEXT` | `false` | Opt-in HTTP non-loopback; jangan di Internet |
| `DASHBOARD_RUN_DIR` | `/run/linux-dashboard` | Socket helper |
| `DASHBOARD_STATE_DIR` | `/var/lib/linux-dashboard` | SQLite web app |
| `DASHBOARD_SOCKET` | `$RUN_DIR/helper.sock` | Path socket helper |
| `DASHBOARD_SOCKET_GROUP` | `linux-dashboard` | Grup yang boleh akses socket |
| `DASHBOARD_SECRET_DIR` | `/var/lib/linux-dashboard-helper` | Secret helper (terpisah dari state web) |
| `DASHBOARD_SECRET` | `$SECRET_DIR/secret.key` | File HMAC (0640, root) |
| `DASHBOARD_DB` | `$STATE_DIR/lindash.db` | Path SQLite |
| `DASHBOARD_SESSION_TTL_HOURS` | `12` | Umur session |
| `DASHBOARD_SECURE_COOKIE` | `false` | Efektif `true` saat TLS native |
| `DASHBOARD_TOTP_KEY` | kosong | Key AES-256-GCM 32-byte untuk TOTP; wajib untuk TFA |

## Model otorisasi

- **Root (UID 0)** selalu diizinkan, dicek sebelum keanggotaan grup (root
  tidak pernah masuk grup `sudo` di Debian/Ubuntu).
- **Anggota grup `sudo`/`admin`**: seluruh operasi privileged.
- **User biasa**: dashboard, file di home sendiri (termasuk `~/DATA/*`),
  hentikan prosesnya, ganti passwordnya, Terminal dengan izin akunnya.
- `~/DATA/*` lokasi data utama; folder per user dibuat otomatis saat File
  Manager dibuka (user A tidak melihat `~/DATA` user B); `Root (/)`
  sudo-only. `%U` tetap ada sebagai mode legacy dan wajib minimal satu user
  Samba; share baru mendapat akun system no-login + password acak + ACL
  read-only/read-write. **Share Guest OK dinonaktifkan** (migrasi otomatis
  saat upgrade).
- Export NFS baru default `ro,sync,no_subtree_check`.
- **TFA** kompatibel Google Authenticator di halaman Akun: secret TOTP
  dienkripsi key terpisah, recovery code sekali tampil, session/capability
  baru diterbitkan setelah faktor kedua.
- Penolakan selalu eksplisit: HTTP 403 kode `requires_sudo`.

## Catatan keamanan

- **Terminal web** = akses SSH penuh lewat browser, dibatasi permission
  Unix akun login. **Hapus sesi** (menutup sesi semua user) butuh verifikasi
  password via PAM → helper daemon komponen paling sensitif.
- **Docker & Components mensyaratkan sudo** (`docker.sock` = root).
- **Token tunnel tidak pernah ditampilkan utuh** (Cloudflare/Tailscale hanya
  bentuk tersamar; Tailscale tidak pernah mengembalikan auth key).
- Menulis konfigurasi sistem selalu via file sementara + `rename`.
- Login dibatasi tiga dimensi, jendela 5 menit: **5** percobaan per
  kombinasi user+IP, **20** per username dari seluruh alamat, **50** per
  IP dengan username apa pun. PAM tidak punya proteksi brute force sendiri.
- **Pakai HTTPS di produksi** (reverse proxy Caddy/Nginx, atau TLS langsung).

## Testing

```bash
make test     # go test ./... + unittest Python grounded_search + npm test
make lint     # go vet ./...
```

Sebagian test helper menyentuh sistem sungguhan dan **skip otomatis** bila
bukan root / paket belum terpasang — aman di mesin dev biasa.

Cakupan terjemahan (dari `web/ui`):

```bash
node scripts/cek-terjemahan.mjs   # teks belum tr()/belum ada padanan Inggris
sh   scripts/cek-runtime.sh       # tr()/trf()/pesanError() dijalankan sungguhan
```

## Target Makefile

| Target | Efek |
|---|---|
| `make` / `make all` | Alias `make build` |
| `make build` | `ui` + `server` + `helper` (urutan wajib) |
| `make ui` | `npm ci` + `vite build` ke `web/dist` (+ audit npm) |
| `make server` | Web app, `CGO_ENABLED=0` |
| `make helper` | Helper daemon, `CGO_ENABLED=1` |
| `make release-server` | Web app amd64 + arm64 + armhf |
| `make install` | `build` lalu `./deploy/install.sh` (jalankan dengan sudo) |
| `make dev` | Cetak tiga perintah terminal terpisah |
| `make clean` | Hapus `bin/` dan `web/dist/assets` |

## Lisensi

MIT License — lihat [LICENSE](LICENSE).

Boleh dipakai, dimodifikasi, dan disebarluaskan gratis, termasuk komersial,
dengan syarat menyertakan sumber: `Copyright (c) 2026 OxidiLily` dan notice
lisensi MIT pada setiap salinan atau bagian penting dari Software.
