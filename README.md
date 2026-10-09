**Bahasa Indonesia** · [English](README.en.md)

# Linux Server Dashboard
Panel Go + React untuk satu server Ubuntu/Debian: homelab, NAS, atau server kecil.

## Fitur panel

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

## Kelebihan

- UI terpadu dan responsif ID/EN; akun Linux existing melalui PAM.
- File/terminal mengikuti permission akun. Web non-root, helper root terpisah lewat Unix socket + HMAC; aksi admin membutuhkan root atau grup `sudo`/`admin`.
- Komponen sesuai kebutuhan; software existing dikenali, halaman dijaga jika dependency belum ada.
- Terminal/AI bertahan saat pindah halaman, bukan reload browser.

## Kekurangan
Masih berkembang, bukan multi-server. Bergantung tooling Linux/perangkat;
build dan layanan tambahan membutuhkan resource/setup. Panel bukan pengganti backup.
HTTP default mengekspos password/OTP/sesi: aktifkan HTTPS sebelum akses publik.
Docker setara root; format/purge menghapus data; UFW INPUT tidak melindungi port Docker.
GeoIP mengirim IP publik diblokir ke ipwho.is. mailcow baru membuka :8080/:8443;
HTTPS awal self-signed: ganti password admin, aktifkan 2FA, siapkan firewall/TLS email.

## Instalasi
```bash
cd / && curl -fsSL https://raw.githubusercontent.com/OxidiLily/linux-dashboard/main/deploy/install.sh | sudo bash
```
Buka `http://<IP-server>:1122`; login akun Linux. Batasi jaringan tepercaya.

## Operasional & pengembangan

- Update panel membandingkan perubahan file, bukan SHA saja: README/Markdown root dan `docs/` tidak memicu; kode, dependency, installer dan aset runtime tetap memicu. Jika fetch/perbandingan gagal, status tetap konservatif.
- Config: `/etc/default/linux-dashboard`; unit `linux-dashboard-web.service` dan `linux-dashboard-helper.service`.
- HTTPS reverse proxy: upstream panel HTTP tetap `http`; set `DASHBOARD_SECURE_COOKIE=true`. Cert/key native existing tetap mengaktifkan TLS.
- Supabase mendeklarasikan port 8000/5432/6543 untuk firewall; batasi akses DB. Edit `.env` perlu **Up**, bukan Restart.
- mailcow membutuhkan FQDN/DNS email dan resource sesuai [upstream](https://docs.mailcow.email/getstarted/prerequisite-system/); LXC/OpenVZ tidak didukung upstream.
- AI perlu login/provider yang sesuai; Hermes/OpenClaw dapat disiapkan ke 9router bila key aktif tersedia. State per akun: `~/DATA/AppData/linux-dashboard`.
- Build: Go 1.26.6+, Node.js 24.15+ atau >=26, compiler C, `make`, `libpam0g-dev`. UI ter-embed perlu rebuild/deploy untuk tampil live.

```bash
make build
make test
make lint
python3 -B -m unittest discover -s deploy -p 'test_*.py'
```

Sebagian tes dapat skip tanpa root/paket/perangkat; gate lokal bukan bukti deployment/E2E.

[MIT](LICENSE) — `Copyright (c) 2026 OxidiLily`; pertahankan notice lisensi.
