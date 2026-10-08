**Bahasa Indonesia** · [English](README.en.md)

# Linux Server Dashboard
Panel Go + React untuk satu server Ubuntu/Debian: homelab, NAS, atau server kecil.

## Fitur inti
Monitoring real-time; file/disk dan Samba/NFS; Docker/Compose; terminal/AI;
firewall/Fail2ban; proxy HTTPS/DNS; cronjob, cetak, dan komponen opsional.

## Kelebihan
Satu UI responsif ID/EN. Login akun Linux, izin sesuai akun.
Web non-root dengan helper privileged terpisah; komponen dipasang sesuai kebutuhan.

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

[MIT](LICENSE) — `Copyright (c) 2026 OxidiLily`; pertahankan notice lisensi.
