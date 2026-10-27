# Linux Server Dashboard

**Bahasa Indonesia** · [English](README.en.md)

> **Disclaimer:** Proyek ini masih dalam tahap pengembangan. Mohon maaf jika masih terdapat banyak bug pada beberapa perangkat.

## Tech Stack

- Backend: Go, chi, WebSocket, PAM (cgo), gopsutil, SQLite.
- Frontend: React 18, TypeScript, Vite, Tailwind CSS, Zustand, xterm.js.
- Sistem: systemd; helper root terpisah dari web non-root melalui Unix socket + HMAC.

## Requirements

- Ubuntu/Debian dengan systemd; target amd64/arm64/armhf.
- Instalasi membutuhkan root/sudo, akses jaringan untuk dependency; aksi admin membutuhkan root atau grup `sudo`/`admin`.
- Build: Go 1.27.1+, Node.js 24.15+ atau >=26, `make`, compiler C, `libpam0g-dev`.
- Build di server dapat membutuhkan memori besar; layanan opsional punya kebutuhan resource/perangkat sendiri. Tidak ada jaminan RAM 1 GB cukup.

## Description

Panel browser untuk **satu server**: homelab, NAS, atau server kecil.
Login menggunakan akun Linux existing, UI responsif ID/EN, komponen sesuai kebutuhan.
Terminal/AI bertahan saat pindah halaman, bukan reload browser.
Masih berkembang; bukan pengelola multi-server atau pengganti backup.

## Installation

Installer memasang dependency/unit systemd, mengaktifkan UFW/fail2ban tanpa mereset rule existing.

```bash
cd / && curl -fsSL https://raw.githubusercontent.com/OxidiLily/linux-dashboard/main/deploy/install.sh | sudo bash
```

Buka `http://<IP-server>:1122`, login akun Linux. Perintah sama untuk upgrade ke `main`.
Instalasi baru HTTP; aktifkan HTTPS sebelum akses publik. Konfigurasi existing dipertahankan.

## Features

| Halaman | Kemampuan |
|---|---|
| Dashboard | Monitoring CPU, RAM, storage, GPU, jaringan; format/mount disk yang memenuhi validasi panel. |
| File Manager | Editor, upload/download, copy/move/delete, rename, permission, pencarian nama rekursif, bookmark. |
| Samba/NFS/Disk Pool | Share Samba berautentikasi, export/client NFS, pool mergerfs. |
| Docker | Container/Compose, log dan terminal container, editor konfigurasi yang diizinkan, image/volume/network, pemakaian disk. |
| Terminal/Processes | Shell akun Linux, daftar proses dan penghentian sesuai privilege. |
| AI Agent | Sesi Claude Code, Codex, OpenCode, Hermes, OpenClaw; tooling per akun dan integrasi 9router. |
| Network | Pengaturan jaringan/DNS, Tailscale, Cloudflare Tunnel. |
| Firewall/Fail2ban | Rule UFW, jail, IP diblokir, detail/riwayat/GeoIP, unban terkonfirmasi, adopsi jail external. |
| Proxy manager | Proxy domain/IPv4, upstream, uji/reload nginx, HTTPS Certbot HTTP-01/DNS-01 Cloudflare, record DNS. |
| Print server | Deteksi printer/driver, antrean CUPS, cetak dari File Manager. |
| Cronjob | Crontab akun login, deteksi perubahan bersamaan, status scheduler. |
| Logs/Alerts | Ambang alert, notifikasi, operasi file, audit login/aksi admin. |
| Account | Pengaturan akun/password, TOTP/recovery codes, bahasa/zona waktu. |
| Components/Updates | Pasang/copot software, progres pekerjaan, pembaruan panel/alat AI; stack resmi Supabase/mailcow. |

Update panel mengabaikan README/Markdown root dan `docs/`; kode, dependency, installer,
aset runtime tetap memicu. Jika fetch/perbandingan gagal, status tetap konservatif.

## Configuration

Config service: `/etc/default/linux-dashboard`. Unit: `linux-dashboard-web.service`, `linux-dashboard-helper.service`.

| Variabel | Fungsi |
|---|---|
| `DASHBOARD_LISTEN` | Bind aplikasi; default kode `127.0.0.1:8080`, installer `0.0.0.0:1122`. |
| `DASHBOARD_ALLOW_PLAINTEXT` | Izinkan HTTP non-loopback; instalasi baru mengaktifkannya. |
| `DASHBOARD_TLS_CERT` / `DASHBOARD_TLS_KEY` | Pasangan TLS native; jika terisi tetap mengaktifkan TLS meski plaintext diizinkan. |
| `DASHBOARD_SECURE_COOKIE` | Set `true` untuk browser HTTPS melalui reverse proxy; TLS native memaksanya `true`. |
| `DASHBOARD_SESSION_TTL_HOURS` | Umur sesi; default 12 jam. |

Untuk reverse proxy HTTPS, upstream panel HTTP tetap `http`; kosongkan cert/key native jika memilih HTTP upstream.
State web: `/var/lib/linux-dashboard`; helper: `/var/lib/linux-dashboard-helper`;
state AI per akun: `~/DATA/AppData/linux-dashboard`.
Supabase: batasi port DB 5432/6543; edit `.env` perlu **Up**, bukan Restart.
AI membutuhkan login/provider; Hermes/OpenClaw dapat memakai 9router bila key aktif tersedia.

## Project Structure

```text
cmd/server/             entry point web
cmd/helper/             entry point helper
internal/api/           REST API dan WebSocket
internal/helper/        operasi privileged
internal/helperproto/   kontrak RPC
internal/helperclient/  client helper
internal/config/        konfigurasi environment
internal/metrics/       collector sistem
internal/store/         SQLite
internal/terminal/      sesi terminal
internal/totp/          autentikasi dua faktor
web/ui/                 frontend React
web/embed.go            embed hasil build UI
deploy/                 installer, unit systemd, migrasi
```

Build/verifikasi: `make build`, `make test`, `make lint`,
`python3 -B -m unittest discover -s deploy -p 'test_*.py'`.
UI perlu rebuild/deploy untuk tampil live; tes dapat skip tanpa root/paket/perangkat.

## Security

Perlindungan yang sudah ada di panel ini:

- **Autentikasi PAM** — login memakai akun Linux asli (password & kebijakan sistem, bukan DB panel); percobaan gagal dibatasi per IP dan per username.
- **Sesi aman** — cookie `HttpOnly` + `SameSite=Lax` (`Secure` saat TLS), TTL 12 jam, tiap request ke helper ditandatangani HMAC.
- **Pemisahan privilege** — web non-root, helper root terpisah lewat Unix socket; file/terminal berjalan sebagai user login sehingga izin Unix ditegakkan kernel.
- **Kredensial** — ganti password wajib password lama; perubahan password/akun/grup otomatis mencabut sesi lama; reset user lain butuh sudo.
- **2FA TOTP** + recovery codes; seed terenkripsi dengan key root-owned di luar database.
- **Audit** — login, aksi admin, dan operasi file tercatat di Logs & Alerts.
- **Batas upload** per berkas dan per permintaan; aksi admin hanya untuk grup `sudo`/`admin`.
- **Firewall** — installer mengaktifkan UFW + fail2ban tanpa menyentuh rule existing; HTTPS via TLS native atau reverse proxy.

Batas yang perlu diketahui:

- HTTP tidak mengenkripsi password/OTP/sesi — aktifkan HTTPS sebelum akses publik.
- Docker setara root; format/purge/uninstall dapat menghapus data — cadangkan dahulu.
- UFW INPUT tidak otomatis melindungi published ports Docker.
- GeoIP mengirim IP publik yang diblokir ke `https://ipwho.is/` dan bendera dari `https://cdn.ipwhois.io` (lokasi hanya estimasi).
- mailcow membuka :8080/:8443 dengan HTTPS self-signed — ganti password admin, aktifkan 2FA, siapkan TLS/firewall sebelum publik ([kebutuhan upstream](https://docs.mailcow.email/getstarted/prerequisite-system/); LXC/OpenVZ tidak didukung).
- Pemisahan privilege mengurangi dampak bug, bukan jaminan bebas serangan.

## License

[MIT](LICENSE) — `Copyright (c) 2026 OxidiLily`; sertakan atribusi dan notice lisensi.
