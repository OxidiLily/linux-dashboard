**Bahasa Indonesia** · [English](README.en.md)

# Linux Server Dashboard (Go + React)

Panel web untuk memonitor dan mengelola satu server Linux: metrik real-time,
file manager, berbagi file (Samba/NFS/mergerfs), print server (CUPS), proses,
Docker, terminal, firewall, dan pengaturan sistem. Login memakai akun Linux yang
sudah ada di mesin — tidak ada tabel user terpisah.

Target: **Ubuntu & Debian**, arsitektur **amd64 / arm64 / armhf**, dirancang
agar tetap ringan di mesin **2 core**.

> **Disclaimer:** project ini masih pada tahap pengembangan, jadi mohon maaf
> jika ada bug.

## Instalasi cepat

```bash
curl -fsSL https://raw.githubusercontent.com/OxidiLily/linux-dashboard/main/deploy/install.sh | sudo bash
```

Skrip memasang dependency build (Go, Node 24, `libpam0g-dev`), mengambil sumber
ke `/usr/local/src/go-react-linux-dashboard`, build UI + dua binary, memasang
unit systemd + file PAM, lalu menyalakan service di port **1122**. Menjalankan
perintah yang sama lagi = upgrade ke `main` terbaru.

Installer **mendeteksi dulu, baru memasang**: dependency yang sudah ada
dilewati, dan Go yang dipasang di luar apt (tarball resmi, asdf, snap) tidak
diganti paket `golang-go`. Di akhir instalasi ditampilkan komponen opsional
mana yang sudah ada dan mana yang perlu dipasang dari menu Components.

Login memakai akun Linux yang sudah ada di mesin (butuh akun bergrup `sudo`
untuk menu Docker, Firewall, Fail2ban, Samba, Disk Pool, NFS, dan Components).

---

## Menu

| Grup | Menu |
|---|---|
| Home | Dashboard (CPU, RAM, Storage, GPU, Network real-time; disk kosong bisa diformat & di-mount dari sini) |
| File manager | File Manager (editor teks, buat file, cetak berkas) · Samba (share + user) · Disk Pool (mergerfs) · NFS Exports · Bookmarks |
| AI | AI Agent (sesi CLI agent di dalam panel: claude-code, codex, opencode, hermes, openclaw) |
| Logs | Logs (semua alert panel) · File Operations · Activity Logs |
| Settings | Network (DNS + Tailscale/Cloudflare Tunnel/WireGuard) · Firewall (ufw) · Fail2ban · Alert Thresholds · Print server (CUPS) · Components |
| System | Processes · Docker (aksi per container, log, editor compose & `.env`) · Terminal |

**Akun** tidak ada di sidebar: pintu masuknya adalah blok profil di kaki
sidebar, yang membuka menu berisi identitas akun, Akun, Uninstall panel (khusus
sudoer), dan Keluar. Rutenya tetap `/settings/account`.

Halaman yang butuh software tertentu (Samba, ufw, Docker, mergerfs, NFS,
fail2ban) menampilkan **"Belum Terpasang"** dengan tombol ke Components, bukan
daftar kosong atau error `command not found`.

Menu **Logs** berisi tiga sudut pandang dengan masa simpan yang ditegakkan
server, bukan sekadar dijanjikan: **Logs** (semua alert panel — berhasil,
gagal, peringatan, info — bisa disaring per status, **1 bulan**),
**File Operations** (**1 bulan**), dan **Activity Logs** (jejak audit login &
aksi admin, **2 tahun**). Catatan yang lewat umurnya dihapus sendiri lewat
sapuan yang jalan saat server start dan sekali sejam sesudahnya. Halaman Logs
mencatat notifikasi yang benar-benar muncul di layar, jadi kegagalan yang tidak
pernah sampai ke server — validasi di browser, koneksi putus — tetap punya
jejak, lengkap dengan halaman asalnya dan keluaran mentahnya.

Panel dipakai penuh dari **layar HP**: di bawah `lg` sidebar berubah jadi drawer
dengan scrim (ditutup oleh scrim, Escape, atau pemilihan menu), tabel berubah
jadi tumpukan kartu di bawah 640px lewat satu kelas CSS + `data-label` per sel —
struktur `<table>` tetap satu-satunya sumber, jadi urutan kolom dan pembacaan
screen reader tidak punya versi kembar. Rename, Edit teks, dan Ubah Permission
di File Manager punya tombol per-baris di HP, karena menu klik-kanan tidak bisa
diandalkan di layar sentuh.

Antarmuka tersedia penuh dalam **bahasa Indonesia dan Inggris** — termasuk pesan
error yang datang dari backend. Pilihan bahasa dan zona waktu ada di topbar dan
tersimpan per akun di server, bukan di browser.

## Components

32 software opsional yang tidak ikut di instalasi dasar Ubuntu/Debian, bisa
dipasang/dicopot dari panel:

| Kategori | Isi |
|---|---|
| Runtime & tunnel | docker · nodejs · tailscale · cloudflared · wireguard |
| AI & Agent | 9router · hermes · claude-code · codex · opencode · openclaw · rtk · graphify · ponytail · browser-use |
| Database & backend | supabase |
| Berbagi file & jaringan | samba · nfs-server · cifs-utils · avahi · technitium-dns · print-server · mergerfs |
| Keamanan | ufw · fail2ban |
| Monitoring & disk | lm-sensors · smartmontools · nvme-cli · qemu-guest-agent |
| Utilitas | htop · ncdu · fastfetch · restic |

Software yang sudah ada di sistem — dipasang manual atau lewat repo lain —
dikenali apa adanya dan **tidak dipasang ulang**.

Selama instalasi berjalan, kartu komponen menampilkan **bar berpersen** yang
angkanya datang dari apt sendiri (`APT::Status-Fd`), bukan dari stopwatch:
indeks 0–10%, unduh 10–55%, pasang 55–99%, dan angkanya tidak pernah turun.
Skrip installer vendor ikut terbaca: apt yang dipanggil di dalamnya menulis
status ke fd yang sama lewat `APT_CONFIG`, jadi Tailscale pun punya angka.
Selama laporan pertama belum datang — installer npm, atau skrip yang masih
mengunduh berkasnya sendiri — yang berjalan sepotong kecil isian menyeberangi
jalur, dan keterangannya menyebut langkah yang sedang dikerjakan ("mengunduh
dan memasang paket npm"). Jalur yang terisi penuh sengaja dihindari: itu
terbaca sebagai pekerjaan 100% yang menggantung. Selama aksinya berjalan,
badge kartu berbunyi "Sedang dipasang", bukan "Belum Terpasang" yang berdiri
di sebelah bar yang sedang jalan.

**Mencopot komponen** menawarkan centang "hapus data juga", tapi hanya untuk
komponen yang memang menyimpan sesuatu di luar paketnya (ditandai `has_data`
dari helper). Default-nya mati, karena yang dihapus tidak bisa dikembalikan.
Contoh gunanya: `~/.9router` menyimpan password yang sudah diganti user, dan
selama folder itu ada, install ulang tidak akan pernah mengembalikan password
awal.

`cloudflared` adalah satu-satunya komponen yang **tidak** punya tombol
Jalankan/Hentikan di halaman ini: tunnel-nya tidak berarti apa-apa tanpa token,
dan tokennya diisi di Settings → Network — jadi kendalinya ada di sana, halaman
Components hanya menampilkan statusnya. Mencopotnya ikut membuang unit systemd
`cloudflared.service` yang ditulis `cloudflared service install <token>`;
token tunnel ada di dalam unit itu dan bukan bagian dari paket .deb, jadi tanpa
langkah ini kunci tunnel lama tetap tertinggal di mesin setelah uninstall.

Uninstall panel mode **"Hapus total"** mencopot seluruh komponen yang bisa
dipasang panel — termasuk Docker, Node.js, Tailscale, cloudflared, dan alat AI
— berikut datanya, memakai uninstaller yang sama dengan halaman Components.
Image dan volume Docker di `/var/lib/docker` tetap ditinggalkan: isinya milik
container Anda, bukan milik panel.

### Supabase self-hosted

Komponen `supabase` memasang backend Supabase lengkap — Postgres, Auth
(GoTrue), PostgREST, Realtime, Storage, Edge Functions, dan Studio — sebagai
stack Docker Compose di `/opt/supabase/supabase-project`. Yang dijalankan panel
adalah **setup.sh resmi** (`curl -fsSL https://supabase.link/setup.sh | sh`,
di sini diunduh ke berkas dulu lalu dieksekusi `sh setup.sh -y`), mengikuti
<https://supabase.com/docs/guides/self-hosting/docker>. Skrip itu yang
melakukan sparse-clone folder `docker/` dari tag rilis self-hosted terbaru dan
membangkitkan seluruh rahasianya lewat `utils/generate-keys.sh` dan
`utils/add-new-auth-keys.sh` — JWT_SECRET, ANON_KEY, SERVICE_ROLE_KEY,
POSTGRES_PASSWORD, dan DASHBOARD_PASSWORD. Panel tidak pernah menyusun
compose atau kuncinya sendiri: bagian yang paling gampang tertinggal saat
Supabase merilis versi baru justru pembangkitan kunci, dan salah di situ
berarti deployment terbuka untuk siapa pun.

Tiga hal yang dikerjakan panel di sekitarnya:

1. **Docker dipasang lewat jalur panel**, bukan dibiarkan ke setup.sh — supaya
   akun yang menekan Pasang ikut masuk grup `docker` dan halaman
   System → Docker benar-benar bisa mengelola stack yang baru dibuat.
2. **URL publik diarahkan ke IP LAN mesin ini.** Bawaan `.env.example` adalah
   `http://localhost:8000`; nilai itu dipakai BROWSER untuk memanggil API,
   jadi dibiarkan apa adanya Studio hanya bekerja dari server itu sendiri.
   Yang diganti hanya `SUPABASE_PUBLIC_URL` dan `API_EXTERNAL_URL` —
   `SITE_URL` menunjuk aplikasi milik Anda, bukan Supabase.
3. **Stack dinyalakan sekali** dengan `sh run.sh start --wait-timeout 600`
   (pembungkus resmi Supabase untuk `docker compose up -d --wait`), sehingga
   sesudah Pasang selesai stack-nya sudah muncul dan bisa dikelola di
   System → Docker.

Hanya port **8000** (gateway Kong/Envoy — Studio, REST, Auth, Realtime, dan
Storage semuanya lewat sana) yang didaftarkan ke ufw. Postgres 5432 dan pooler
6543 juga terbuka di compose bawaan, tapi mengizinkannya ke seluruh LAN adalah
keputusan admin di Settings → Firewall, bukan efek samping menekan Pasang.
Tombol **Buka** di kartu komponen mengarah ke `http://<host panel>:8000`;
kredensial Studio ada di `DASHBOARD_USERNAME`/`DASHBOARD_PASSWORD` dan bisa
dibaca lewat penyunting `.env` stack di halaman System → Docker.

**Mencopotnya tidak menghapus database.** Seluruh data Supabase ada di dalam
folder proyek (`volumes/db/data`, `volumes/storage`, dan `.env` yang memuat
JWT_SECRET), jadi uninstall biasa menghentikan stack lalu *memindahkan*
foldernya ke `/opt/supabase/bekas-<tanggal>-<jam>` — kartunya kembali ke
"belum terpasang", pemasangan berikutnya tidak ditolak setup.sh, dan datanya
masih ada kalau ternyata masih dibutuhkan. Centang "hapus data juga" yang
membuang `/opt/supabase` seluruhnya, termasuk folder `bekas-*`.

### Alat & skill wajib AI Agent

Empat komponen terakhir di kategori AI — `rtk`, `graphify`, `ponytail`,
`browser-use` — bukan agent, melainkan alat yang dipakai **semua** agent.
Keempatnya dipasang otomatis
begitu agent mana pun dipasang, dan arahan pemakaiannya ditulis ke berkas
instruksi global tiap agent (`~/.claude/CLAUDE.md`, `~/.codex/AGENTS.md`, dan
seterusnya) setiap sesi AI Agent dibuka — jadi akun panel yang dibuat setelah
instalasi pun ikut mendapatkannya.

| Alat | Peran | Dokumentasi |
|---|---|---|
| rtk | Memangkas keluaran perintah shell sebelum masuk konteks agent | <https://github.com/rtk-ai/rtk#quick-start> |
| graphify | Knowledge graph kode lewat parsing AST lokal | <https://github.com/Graphify-Labs/graphify#install> |
| ponytail | Harness "lazy senior dev" level ultra + skill audit/review/debt | <https://github.com/DietrichGebert/ponytail#install> |
| browser-use | Kendali browser lewat CDP — halaman ber-JavaScript, login, klik, isi form | <https://browser-use.com> · <https://docs.browser-use.com> |

Pendaftaran ke agent dilakukan **per user dan per agent**, tepat sebelum sesi
AI Agent dibuka — bukan sekali saat instalasi. Daemon helper berjalan sebagai
root, jadi `rtk init -g` yang dipanggil installer hanya menambal `/root`;
akun panel lain membuka agent dengan HOME miliknya sendiri. Target yang
dipakai per agent:

| Agent panel | rtk | graphify | browser-use |
|---|---|---|---|
| claude-code | `rtk init -g --auto-patch --no-trust-filters` | `graphify install --platform claude` | `--target claude` |
| codex | `rtk init -g --codex` | `graphify install --platform codex` | `--target codex` |
| opencode | `rtk init -g --opencode --auto-patch --no-trust-filters` | `graphify install --platform opencode` | `--target opencode` |
| hermes | `rtk init -g --agent hermes` | `graphify install --platform hermes` | — (belum punya direktori skill) |
| openclaw | — (rtk belum punya target OpenClaw) | `graphify install --platform claw` | `--target openclaw` |

`--auto-patch` dan `--no-trust-filters` wajib untuk target yang menambal
`settings.json`: tanpa keduanya rtk bertanya ke terminal, dan daemon tidak
punya siapa pun untuk menjawab.

Kolom browser-use adalah argumen `browser-use skill install --no-install
--target <nilai>`, yang menulis `SKILL.md` resmi ke direktori skill agent itu.
`--no-install` wajib: tanpanya perintah tersebut memasang salinan browser-use
keduanya sendiri lewat `uv` ke `~/.local/bin`, bersaing dengan yang sudah
dipasang panel system-wide. hermes tidak punya direktori skill di daftar
browser-use, jadi untuknya hanya arahan di `~/.hermes/AGENTS.md` yang berlaku.

### Provider inferensi lewat 9router

Sesi pertama Hermes dan OpenClaw di mesin yang belum dikonfigurasi berhenti
menunggu user memilih provider — Hermes dengan "No inference provider is
configured yet", OpenClaw dengan "no models available". Di panel ini jawabannya
sudah pasti: 9router yang berjalan di mesin yang sama sebagai gateway
OpenAI-compatible. Keduanya karena itu **disambungkan otomatis** saat sesi AI
Agent dibuka, sebagai user pemilik sesi.

| Agent | Disambungkan | Cara |
|---|---|---|
| hermes | otomatis | `~/.hermes/config.yaml` + `OPENAI_API_KEY` di `~/.hermes/.env` |
| openclaw | otomatis | `openclaw onboard --non-interactive --auth-choice custom-api-key --custom-provider-id 9router` |
| claude-code | tidak — opsional, manual | blok `env` di `~/.claude/settings.json` |
| codex · opencode | tidak — opsional, manual | provider `9router` di config masing-masing |

Claude Code dan Codex/OpenCode sengaja **tidak** diarahkan otomatis: keduanya
punya login sendiri (langganan Anthropic, akun ChatGPT), dan memaksa base
URL-nya ke 9router akan merusak instalasi yang sebenarnya sudah bekerja.
Keduanya tetap bisa dipakai lewat 9router sebagai pilihan sadar user.

API key tidak pernah ditebak. 9router membuat "Default Key" sendiri saat
pertama kali hidup dan menyimpannya di tabel `apiKeys` pada
`~/.9router/db/data.sqlite`; panel membacanya dari sana. Perlu diketahui saat
menguji sendiri: `GET /v1/models` menjawab 200 **tanpa** header Authorization,
sedangkan `POST /v1/chat/completions` menolak dengan 401 — jadi endpoint chat
yang menentukan, bukan endpoint models.

Config yang sudah menyebut provider tidak pernah ditimpa: user yang sengaja
pindah ke Anthropic, OpenAI, atau model lokal lain tetap di sana.

#### Memakai 9router di Claude Code (opsional)

Ganti `sk-…` dengan API key dari halaman 9router, dan sesuaikan nama model
dengan yang tersedia di gateway.

```json
{
  "hasCompletedOnboarding": true,
  "env": {
    "ANTHROPIC_BASE_URL": "http://127.0.0.1:20128/v1",
    "ANTHROPIC_AUTH_TOKEN": "sk-…",
    "ANTHROPIC_DEFAULT_FABLE_MODEL": "cc/claude-fable-5",
    "ANTHROPIC_DEFAULT_OPUS_MODEL": "cc/claude-opus-5",
    "ANTHROPIC_DEFAULT_SONNET_MODEL": "cc/claude-sonnet-5",
    "ANTHROPIC_DEFAULT_HAIKU_MODEL": "cc/claude-haiku-4-5-20251001"
  }
}
```

Berkas ini sama dengan yang ditambal `rtk init -g --auto-patch`; blok `env` di
atas hidup berdampingan dengan hook rtk.

#### Memakai 9router di OpenCode (opsional)

`provider/model-id` adalah placeholder — isi dengan id model yang dilaporkan
`GET /v1/models` milik gateway.

```json
{
  "provider": {
    "9router": {
      "npm": "@ai-sdk/openai-compatible",
      "options": {
        "baseURL": "http://127.0.0.1:20128/v1",
        "apiKey": "sk-…"
      },
      "models": {
        "provider/model-id": {
          "name": "provider/model-id",
          "modalities": { "input": ["text", "image"], "output": ["text"] }
        }
      }
    }
  },
  "model": "9router/provider/model-id",
  "agent": {
    "explorer": {
      "description": "Fast explorer subagent for codebase exploration",
      "mode": "subagent",
      "model": "9router/provider/model-id"
    }
  }
}
```

## Print server (CUPS)

`print-server` adalah komponen **opsional** dan sengaja tidak ikut instalasi
dasar: panel ini dipakai di segala jenis mesin, dan di LXC print server memang
tidak bisa berjalan sama sekali. Mesin yang tidak akan pernah mencetak karena
itu tidak dianggap kurang lengkap — halaman Print server cukup menampilkan
"Belum Terpasang" dengan tombol ke Components.

Kalau dipasang, seluruh alur mencetak selesai di dalam panel, tanpa membuka
terminal:

1. **Components → print-server** memasang `cups` + `printer-driver-gutenprint`.
   Gutenprint ikut dipasang, bukan opsional: printer USB rumahan (Canon PIXMA,
   Epson, banyak HP) tidak mendukung IPP Everywhere, dan CUPS akan menerima
   antrean tanpa driver dengan senang hati sebelum tiap cetakan berakhir kosong.
2. **Settings → Print server → Deteksi** menggabungkan tiga hal yang harus
   dilihat bersamaan — perangkat dari `lpinfo`, driver yang tersedia di sistem,
   dan antrean yang sudah ada — sehingga "printer siap didaftarkan" bisa
   dibedakan dari "printer terlihat tapi drivernya belum ada".
3. **Pasang driver** untuk printer yang belum siap. Yang dikirim frontend adalah
   nama **vendor**, bukan nama paket; pemetaan vendor → paket adalah whitelist
   di backend (`internal/helper/printer.go`), jadi endpoint ini tidak pernah
   bisa berubah jadi "frontend memilih paket apa pun untuk dipasang sebagai
   root".
4. **Daftarkan antrean**, lalu **cetak** lewat menu di File Manager (pilih
   printer, jumlah salinan, ukuran media, satu/dua sisi) dan pantau antreannya.

Pembagian sudo di halaman ini sengaja tidak seragam: mengubah daftar printer,
memindai perangkat, dan memasang driver butuh sudo karena mengubah konfigurasi
mesin untuk semua orang. Melihat printer, melihat antrean, dan mencetak berkas
sendiri tidak — itu justru alasan fitur ini ada.

Mencetak **tidak** membuka berkas sebagai root: setelah pemeriksaan path, isinya
dialirkan lewat worker yang privilegenya sudah diturunkan ke user login lalu
masuk ke stdin `lp`. Skema device `file://` ditolak — itu bukan printer, itu
cara menyuruh `cupsd` menulis berkas sembarangan sebagai root.

Penemuan printer jaringan lewat mDNS bergantung pada Avahi. Avahi sengaja
**tidak** dijadikan dependensi print-server (ia mengubah perilaku jaringan mesin
lebih luas daripada sekadar mencetak), jadi halaman Deteksi menyebutkan kondisi
itu beserta jalan keluarnya kalau Avahi belum ada.

---

## Firewall: port komponen didaftarkan sebelum firewall menyala

`ufw` dipasang dengan `DEFAULT_INPUT_POLICY=DROP`, jadi menyalakannya memutus
setiap layanan yang portnya belum diizinkan. Karena itu port **dideklarasikan
per komponen** dan didaftarkan saat komponennya dipasang, bukan saat firewall
dinyalakan — `ufw allow` tetap tersimpan di `/etc/ufw/user.rules` meski ufw
sedang nonaktif:

| Komponen | Port |
|---|---|
| samba | 445/tcp · 139/tcp · 137:138/udp |
| nfs-server | 2049/tcp · 111/tcp · 111/udp |
| avahi | 5353/udp |
| print-server | 631/tcp |
| 9router | 20128/tcp |

Sumbernya dibatasi ke subnet lokal yang dideteksi dari default route; port SSH
dan port panel sengaja dibiarkan `Anywhere`, karena admin bisa masuk dari subnet
lain. Memasang `ufw` belakangan tidak membuat komponen yang sudah ada tertinggal
— saat itu seluruh port komponen yang terpasang didaftarkan menyusul. Mencopot
komponen mencabut izinnya lagi.

fail2ban tidak menyediakan filter untuk satu pun komponen di katalog, jadi jail
`sshd` dinyalakan otomatis saat fail2ban dipasang, dan filter Samba dipasang
panel sendiri. Filter itu hanya berguna kalau kegagalan login benar-benar
tercatat, sementara `map to guest = Bad User` bawaan Ubuntu memetakan username
tak dikenal ke guest tanpa satu pun baris `NT_STATUS_LOGON_FAILURE` — karena itu
blok berisi `map to guest = Never` dan `log level = 0 auth_audit:3` disisipkan di
**akhir** section `[global]` `smb.conf` (Samba memakai nilai terakhir dalam satu
section, jadi setelan panel menang tanpa mengedit baris milik admin, dan membuang
blok itu mengembalikan konfigurasi lama persis).

Aturan dan jail yang dibuat **sesudahnya milik user**: `ufw enable` tidak
mendaftarkan ulang port komponen, dan jail yang sudah ada atau sudah dihapus
tidak dibuat ulang. Yang tetap dipastikan sebelum firewall menyala hanya akses
admin, karena kehilangan itu berarti kehilangan mesinnya.

---

## Disk & Disk Pool

Disk mentah yang belum dipakai ikut dilaporkan collector (`unused_disks`: tanpa
partisi, tanpa holder LVM/RAID, tidak ter-mount) dan muncul di kartu Storage
dashboard. Kapasitasnya sengaja **tidak** ikut total storage — ruang itu belum
bisa dipakai, jadi memasukkannya akan membuat persentase pemakaian bohong.

Mengklik disk itu membuka dialog format & mount: pilih mount point dan
filesystem (ext4/xfs/btrfs), lalu helper menjalankan `mkfs`, menulis entri
`/etc/fstab` lewat UUID dengan opsi `nofail`, dan me-mount-nya. Pagarnya: hanya
disk yang diakui `UnusedDisks()` yang boleh disentuh (daftar yang sama persis
dengan yang dipakai dashboard), disk yang ternyata sudah berisi filesystem
ditolak dengan kode `disk_has_filesystem` lalu dialognya menawarkan mount tanpa
format, dan `fstab` ditulis atomik lalu dikembalikan kalau mount gagal.

**Disk Pool (mergerfs)** menggabungkan beberapa disk jadi satu mount point.
Yang perlu diketahui:

- Kebijakan bawaannya `category.create=pfrd` — berkas baru disebar acak dengan
  bobot sisa ruang, bukan ditumpuk di satu disk seperti `mfs`. Pool yang sudah
  ada tidak ikut berubah; opsinya tersimpan di `/etc/fstab` dan hanya berubah
  lewat Edit.
- Pool bisa **dipasang/dilepas** tanpa menghapus definisinya. Operasinya
  idempoten dan tidak menyentuh `/etc/fstab`, jadi pool yang dilepas terpasang
  lagi setelah boot.
- Mount point **dikunci immutable** setiap kali direktorinya telanjang (sebelum
  mount dan sesudah umount). Tanpa itu, berkas yang diunggah saat pool lepas
  memenuhi disk sistem lalu tersembunyi begitu pool dipasang lagi.
- Melepas atau menghapus pool ikut membuang folder mount point-nya kalau kosong;
  folder yang **tidak** kosong dipertahankan beserta isinya lalu dikunci.
- Tiap pool yang sedang ter-mount muncul sebagai pintasan "Disk pool : &lt;Nama&gt;"
  tepat setelah `Root (/)` di File Manager — sudo-only, sama seperti `Root (/)`.

---

## Konfigurasi milik sistem

Samba share, pool mergerfs, disk yang disiapkan panel, export NFS, jail
fail2ban, dan antrean printer ditulis ke file konfigurasi sistem (`smb.conf`
include, `/etc/fstab`, `/etc/exports`, `jail.local`, `printers.conf` lewat
`lpadmin`). Aturannya sama untuk semuanya:

- baris/section yang **bukan tulisan panel** ikut ditampilkan, ditandai, dan
  bersifat read-only — konfigurasi milik admin tidak pernah ditulis ulang;
- penulisan lewat file sementara lalu `rename`, karena file yang terpotong di
  tengah penulisan bisa membuat sistem gagal boot;
- status yang ditampilkan dibaca dari sistem (`exportfs -s`, `findmnt`,
  `fail2ban-client status`, `lpstat`), bukan dari isi file.

Satu pengecualian yang disengaja: blok `[global]` yang disisipkan panel di
`smb.conf` supaya kegagalan login Samba benar-benar tercatat (lihat bagian
Firewall di atas). Ia ditambahkan di akhir section, bukan menimpa baris admin;
aslinya dicadangkan ke `smb.conf.lindash.bak`, dan hasil yang ditolak `testparm`
dikembalikan otomatis sebelum `smbd` sempat gagal start.

---

## Arsitektur

Dua proses, dipisah berdasarkan privilege:

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

Proses web **tidak pernah** punya akses root. Semua operasi privileged dikirim
sebagai command terstruktur ke helper daemon, ditandatangani HMAC, dan
dieksekusi dengan argumen array — tidak pernah lewat `sh -c`.

Untuk operasi yang harus berjalan **sebagai user yang login** (baca/tulis file
di home, kill proses sendiri, shell terminal), helper mem-fork proses anak
dengan `SysProcAttr.Credential`. Kernel yang menegakkan izin, bukan kode kita —
jadi tidak ada logika permission Unix yang ditiru ulang dan bisa salah.

## Struktur

```
cmd/server          entrypoint web app
cmd/helper          entrypoint helper daemon (+ mode worker)
internal/helperproto  kontrak command antara keduanya
internal/helper       implementasi daemon root (components, samba, mergerfs,
                      nfs, fail2ban, printer, portkomponen, progres, vpn,
                      files, users, docker, terminal)
internal/helperclient client HMAC ke daemon
internal/api          REST handler + WebSocket
internal/metrics      collector gopsutil + deteksi GPU multi-vendor
internal/platform     deteksi OS/kernel/platform (14 skenario)
internal/store        SQLite: session, log, bookmark, threshold, stack
internal/terminal     kuota + daftar sesi terminal berbasis jumlah core
internal/config       konfigurasi dari environment
web/embed.go          go:embed hasil build React
web/ui                sumber frontend (React TSX + Vite + Tailwind v4)
deploy/               unit systemd, file PAM, installer satu baris
```

## Membangun

Butuh **Go 1.26.6+** (versi di `go.mod`; toolchain lama otomatis mengunduh yang
tepat lewat `GOTOOLCHAIN=auto`), **Node.js 20+**, dan `libpam0g-dev` — helper
daemon memakai PAM lewat cgo.

```bash
sudo apt install -y build-essential libpam0g-dev
make build          # build UI → embed → dua binary di bin/
sudo ./deploy/install.sh
```

Tanpa `sudo` di depan pun bisa: skrip mendeteksi dirinya bukan root lalu
menjalankan ulang dirinya sendiri lewat `sudo` (variabel override seperti
`PREFIX` ikut terbawa). Kalau paket `sudo` sendiri belum terpasang, installer
memasangnya — grup `sudo` yang dibuat paket itu yang dipakai panel untuk
menentukan siapa sudoer. Versi yang dipipe dari `curl` tetap harus ditulis
`| sudo bash`, karena tidak ada berkas yang bisa dijalankan ulang.

### Dependensi

**Build** (dipasang otomatis oleh installer kalau belum ada):
`ca-certificates`, `curl`, `git`, `make`, `build-essential`, `libpam0g-dev`,
Go 1.26.6+, Node 24 dari NodeSource (Node 20+ yang sudah ada tidak diganti).

**Runtime dari sistem dasar** — dipakai helper daemon, sudah ada di Ubuntu/Debian
normal; installer memperingatkan kalau image minimal memangkasnya:
`systemctl`, `ip`, `hostnamectl`, `resolvectl`, `findmnt`, `mount`/`umount`,
`useradd`/`usermod`/`userdel`, `apt-get`, `dpkg-query`.

**Runtime opsional** — tidak dipasang installer, dikelola dari menu Components;
halaman yang membutuhkannya menampilkan "Belum Terpasang" sampai dipasang:

| Paket | Binary | Halaman |
|---|---|---|
| samba | `smbd`, `smbpasswd`, `pdbedit`, `testparm` | File manager → Samba |
| mergerfs | `mergerfs` (butuh `/dev/fuse`) | File manager → Disk Pool |
| nfs-kernel-server | `exportfs` | File manager → NFS Exports |
| cups + printer-driver-gutenprint | `cupsd`, `lpadmin`, `lpinfo`, `lpstat`, `lp` | Settings → Print server |
| ufw | `ufw` | Settings → Firewall |
| fail2ban | `fail2ban-client` | Settings → Fail2ban |
| docker-ce + docker-compose-plugin (repo resmi Docker) | `docker` | System → Docker |
| wireguard, tailscale, cloudflared | `wg`/`wg-quick`, `tailscale`, `cloudflared` | Settings → Network |
| nodejs | `node`, `npm` | Components → 9Router |

**Library Go**: `go-chi/chi/v5`, `coder/websocket`, `creack/pty`,
`msteinert/pam/v2` (cgo), `shirou/gopsutil/v4`, `modernc.org/sqlite` (pure Go).
**Frontend**: React 18 + Vite 6 + TypeScript 5.7, Tailwind v4 (`@tailwindcss/vite`),
`@radix-ui/react-slot`, Zustand 5, react-router-dom 6, `@xterm/xterm` 5,
`lucide-react`. Dialog, toast, dan komponen UI lain adalah source TSX milik
proyek di `src/components/ui/` — bukan library pihak ketiga.

`make release-server` mem-build web app untuk amd64, arm64, dan armhf sekaligus
(cross-compile bawaan Go, tanpa toolchain tambahan — web app sengaja
`CGO_ENABLED=0`). Helper daemon memakai PAM lewat cgo, jadi harus di-build
dengan compiler untuk arsitektur targetnya.

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
| `DASHBOARD_LISTEN` | `0.0.0.0:1122` | Alamat bind web app |
| `DASHBOARD_TLS_CERT` | kosong | Sertifikat TLS; kosongkan kalau pakai reverse proxy |
| `DASHBOARD_TLS_KEY` | kosong | Private key TLS; harus diisi bersama `DASHBOARD_TLS_CERT` |
| `DASHBOARD_RUN_DIR` | `/run/linux-dashboard` | Lokasi socket helper |
| `DASHBOARD_STATE_DIR` | `/var/lib/linux-dashboard` | Lokasi SQLite + `secret.key` |
| `DASHBOARD_SOCKET` | `$RUN_DIR/helper.sock` | Path socket helper (override penuh) |
| `DASHBOARD_SOCKET_GROUP` | `linux-dashboard` | Grup yang boleh mengakses socket |
| `DASHBOARD_SECRET` | `$STATE_DIR/secret.key` | File HMAC secret helper (0600, milik root) |
| `DASHBOARD_DB` | `$STATE_DIR/lindash.db` | Path database SQLite |
| `DASHBOARD_SESSION_TTL_HOURS` | `12` | Umur session |
| `DASHBOARD_SECURE_COOKIE` | `false` | Set `true` kalau diakses lewat HTTPS |

## Model otorisasi

- **Root (UID 0)** selalu diizinkan, dicek **sebelum** keanggotaan grup — root
  memang tidak pernah jadi anggota grup `sudo` di Debian/Ubuntu, jadi otorisasi
  yang hanya mengecek grup akan salah menolak root.
- **Anggota grup `sudo`** (atau `admin`) diizinkan untuk operasi privileged.
- **User biasa** tetap bisa: melihat dashboard, mengelola file di home
  directory sendiri (termasuk folder data `~/DATA/*`), menghentikan proses
  miliknya sendiri, mengganti passwordnya sendiri, dan memakai Terminal dengan
  izin akunnya.
- **Installer menyiapkan folder ini untuk akun yang sudah ada** di mesin dan
  menaruh kerangkanya di `/etc/skel`, jadi akun baru — dibuat dari panel maupun
  `useradd -m` di terminal — langsung memilikinya tanpa menunggu login.
- `~/DATA/*` adalah lokasi data utama panel ini. Share Samba bawaan menunjuk ke
  sana lewat makro `%U` (`/home/%U/DATA/Documents`), sehingga satu share memberi
  tiap akun folder datanya sendiri.
- **Folder data per user** (`~/DATA/AppData`, `~/DATA/Documents`,
  `~/DATA/Downloads`, `~/DATA/Gallery`, `~/DATA/Media`) dibuat otomatis saat
  File Manager dibuka dan muncul di sana sebagai root tersendiri. Semuanya
  ada di dalam home masing-masing akun, jadi tidak ada folder yang dipakai
  bersama: user A tidak melihat `~/DATA` milik user B. Isinya dibaca **sebagai
  user yang login** — entri yang tidak bisa ia buka disembunyikan dari daftar.
  `Root (/)` tetap sudo-only.
- Penolakan selalu eksplisit: HTTP 403 dengan kode `requires_sudo` dan pesan
  "Aksi ini butuh akses sudo" — tidak pernah gagal diam-diam.

## Catatan keamanan

- **Terminal web** setara akses SSH penuh lewat browser, dibatasi murni oleh
  permission Unix akun yang login. Tombol **Hapus sesi** di header Terminal
  menutup semua sesi sekaligus (termasuk milik user lain), jadi panel meminta
  password akun dan memverifikasinya lewat PAM sebelum menjalankannya. Ini
  keputusan produk yang disengaja, tapi membuat helper daemon jadi komponen
  paling sensitif di sistem.
- **Menu Docker mensyaratkan sudo.** Akses ke `docker.sock` setara root karena
  container bisa mem-bind mount filesystem host.
- **Menu Components mensyaratkan sudo** — memasang paket mengubah sistem secara
  permanen.
- **Kredensial tunnel tidak pernah ditampilkan utuh.** Token Cloudflare Tunnel
  dan auth key Tailscale dikirim ke browser dalam bentuk tersamar
  (`eyJhIjoiZ...xxxxxxxxxxxxxxxx`); yang utuh tidak pernah meninggalkan helper
  daemon. Tailscale tidak pernah mengembalikan auth key-nya sama sekali, jadi
  panel hanya menyimpan bentuk tersamarnya.
- **Menulis konfigurasi sistem selalu lewat file sementara + `rename`,** dan
  baris milik admin tidak pernah disentuh — `/etc/fstab` atau `/etc/exports`
  yang rusak bisa membuat server gagal boot atau membuka data ke host yang
  tidak diizinkan.
- Login dibatasi 5 percobaan per 5 menit per kombinasi user + IP. PAM tidak
  menyediakan proteksi brute force sendiri.
- **Pakai HTTPS di produksi** (reverse proxy Caddy/Nginx, atau TLS langsung).

## Testing

```bash
make test     # go test ./... + npm run test --if-present
make lint     # go vet ./...
```

Sebagian test helper (user Linux, Samba, ufw, fail2ban, mergerfs, NFS) menyentuh
sistem sungguhan dan **skip otomatis** kalau tidak dijalankan sebagai root atau
kalau paket yang diuji belum terpasang — jadi `make test` aman dijalankan di mesin
pengembangan biasa.

Cakupan terjemahan diperiksa terpisah, dari `web/ui`:

```bash
node scripts/cek-terjemahan.mjs   # teks UI yang belum dibungkus tr()/belum punya padanan Inggris
sh   scripts/cek-runtime.sh       # tr()/trf()/pesanError() + logika kecil di view dijalankan sungguhan
```

## Target Makefile

| Target | Efek |
|---|---|
| `make` / `make all` | Alias `make build` |
| `make build` | `ui` + `server` + `helper` — urutan wajib, binary meng-embed hasil build UI |
| `make ui` | `npm ci` lalu `vite build` ke `web/dist` |
| `make server` | Web app, `CGO_ENABLED=0` (bisa cross-compile) |
| `make helper` | Helper daemon, `CGO_ENABLED=1` (PAM lewat cgo) |
| `make release-server` | Web app untuk amd64 + arm64 + armhf sekaligus |
| `make install` | `build` lalu `./deploy/install.sh` dari checkout (jalankan dengan sudo) |
| `make dev` | Mencetak tiga perintah yang harus dijalankan di terminal terpisah (helper, server, `vite dev`) |
| `make clean` | Hapus `bin/` dan `web/dist/assets` |
