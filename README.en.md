[Bahasa Indonesia](README.md) · **English**

# Linux Server Dashboard
A Go + React panel for one Ubuntu/Debian server: homelab, NAS, or small server.

## Core features
Real-time monitoring; files/disks and Samba/NFS; Docker/Compose; terminal/AI;
firewall/Fail2ban; HTTPS proxy/DNS; cronjobs, printing, and optional components.

## Strengths
One responsive ID/EN interface. Linux account login and account-level permissions.
Non-root web process with a separate privileged helper; install components as needed.

## Weaknesses
Under development, not multi-server. Depends on Linux tooling/hardware;
builds and extra services need resources/setup. The panel does not replace backups.
Default HTTP exposes passwords/OTPs/sessions: enable HTTPS before public access.
Docker is root-equivalent; formatting/purging destroys data; UFW INPUT does not protect Docker ports.
GeoIP sends public banned IPs to ipwho.is. New mailcow exposes :8080/:8443;
initial HTTPS is self-signed: change admin password, enable 2FA, configure firewall/mail TLS.

## Installation
```bash
cd / && curl -fsSL https://raw.githubusercontent.com/OxidiLily/linux-dashboard/main/deploy/install.sh | sudo bash
```
Open `http://<server-IP>:1122`; use a Linux account. Restrict access to trusted networks.

[MIT](LICENSE) — `Copyright (c) 2026 OxidiLily`; retain the license notice.
