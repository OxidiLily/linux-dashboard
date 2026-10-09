# Linux Server Dashboard

[Bahasa Indonesia](README.md) · **English**

> **Disclaimer:** This project is still under development. We apologize if you encounter many bugs on some devices.

## Tech Stack

- Backend: Go, chi, WebSocket, PAM (cgo), gopsutil, SQLite.
- Frontend: React 18, TypeScript, Vite, Tailwind CSS, Zustand, xterm.js.
- System: systemd; separate root helper and non-root web process over Unix socket + HMAC.

## Requirements

- Ubuntu/Debian with systemd; targets amd64/arm64/armhf.
- Installation requires root/sudo and network access for dependencies; admin actions require root or `sudo`/`admin` membership.
- Build: Go 1.26.6+, Node.js 24.15+ or >=26, `make`, a C compiler, `libpam0g-dev`.
- Server-side builds can be memory-intensive; optional services have separate resource/hardware requirements. No guarantee of fitting in 1 GB RAM.

## Description

A browser panel for **one server**: homelab, NAS, or small server.
Existing Linux accounts, responsive ID/EN UI, components installed as needed.
Terminal/AI sessions survive page changes, not browser reloads.
Under development; not a multi-server manager or backup replacement.

## Installation

The installer adds dependencies/systemd units and enables UFW/fail2ban without resetting existing rules.

```bash
cd / && curl -fsSL https://raw.githubusercontent.com/OxidiLily/linux-dashboard/main/deploy/install.sh | sudo bash
```

Open `http://<server-IP>:1122`, use a Linux account. Repeat the command to upgrade to `main`.
New installations use HTTP; enable HTTPS before public access. Existing configuration is preserved.

## Features

| Page | Capabilities |
|---|---|
| Dashboard | CPU, RAM, storage, GPU, network monitoring; format/mount disks accepted by panel validation. |
| File Manager | Editor, uploads/downloads, copy/move/delete, rename, permissions, recursive filename search, bookmarks. |
| Samba/NFS/Disk Pool | Authenticated Samba shares, NFS exports/clients, mergerfs pools. |
| Docker | Containers/Compose, container logs/terminals, permitted configuration editors, images/volumes/networks, disk usage. |
| Terminal/Processes | Linux account shell, process listing and termination according to privileges. |
| AI Agent | Claude Code, Codex, OpenCode, Hermes, OpenClaw sessions; per-account tooling and 9router integration. |
| Network | Network/DNS settings, Tailscale, Cloudflare Tunnel. |
| Firewall/Fail2ban | UFW rules, jails, banned IPs, details/history/GeoIP, confirmed unban, external jail adoption. |
| Proxy manager | Domain/IPv4 proxies, upstreams, nginx test/reload, Certbot HTTP-01/Cloudflare DNS-01 HTTPS, DNS records. |
| Print server | Printer/driver detection, CUPS queues, printing from File Manager. |
| Cronjob | Logged-in account crontab, concurrent-change detection, scheduler status. |
| Logs/Alerts | Alert thresholds, notifications, file operations, login/admin audit logs. |
| Account | Account/password settings, TOTP/recovery codes, language/time zone. |
| Components/Updates | Software installation/removal, job progress, panel/AI tool updates; official Supabase/mailcow stacks. |

Panel updates ignore README/root Markdown and `docs/`; code, dependencies, installer,
and runtime assets still trigger updates. Fetch/comparison failures retain a conservative status.

## Configuration

Service config: `/etc/default/linux-dashboard`. Units: `linux-dashboard-web.service`, `linux-dashboard-helper.service`.

| Variable | Purpose |
|---|---|
| `DASHBOARD_LISTEN` | Application bind; code default `127.0.0.1:8080`, installer `0.0.0.0:1122`. |
| `DASHBOARD_ALLOW_PLAINTEXT` | Allow non-loopback HTTP; enabled for new installations. |
| `DASHBOARD_TLS_CERT` / `DASHBOARD_TLS_KEY` | Native TLS pair; still enables TLS when configured even if plaintext is allowed. |
| `DASHBOARD_SECURE_COOKIE` | Set `true` for HTTPS browser access through a reverse proxy; native TLS forces `true`. |
| `DASHBOARD_SESSION_TTL_HOURS` | Session lifetime; default 12 hours. |

For an HTTPS reverse proxy, keep an HTTP panel upstream on `http`; clear native cert/key when selecting an HTTP upstream.
Web state: `/var/lib/linux-dashboard`; helper: `/var/lib/linux-dashboard-helper`;
per-account AI state: `~/DATA/AppData/linux-dashboard`.
Supabase: restrict DB ports 5432/6543; applying `.env` changes needs **Up**, not Restart.
AI requires logins/providers; Hermes/OpenClaw can use 9router when an active key exists.

## Project Structure

```text
cmd/server/             web entry point
cmd/helper/             helper entry point
internal/api/           REST API and WebSocket
internal/helper/        privileged operations
internal/helperproto/   RPC contract
internal/helperclient/  helper client
internal/config/        environment configuration
internal/metrics/       system collector
internal/store/         SQLite
internal/terminal/      terminal sessions
internal/totp/          two-factor authentication
web/ui/                 React frontend
web/embed.go            embedded UI build
deploy/                 installer, systemd units, migrations
```

Build/check: `make build`, `make test`, `make lint`,
`python3 -B -m unittest discover -s deploy -p 'test_*.py'`.
UI changes need rebuilding/deployment to go live; tests may skip without root/packages/hardware.

## Security

- HTTP does not encrypt passwords, OTPs, or sessions. Restrict access to trusted networks; enable HTTPS before public access.
- Files/terminals follow account permissions; Docker is root-equivalent. Formatting/purging/data uninstall modes can destroy data: back up first.
- UFW INPUT does not automatically protect Docker published ports; configure forwarding firewall rules.
- TOTP/recovery codes are available; privilege separation does not guarantee freedom from bugs or attacks.
- GeoIP sends public banned IPs to `https://ipwho.is/`; flags may load from `https://cdn.ipwhois.io`. Location is an estimate.
- New mailcow exposes HTTP :8080/HTTPS :8443 on all interfaces, initially using self-signed HTTPS. Change the admin password, enable 2FA, configure firewall/SMTP/IMAP TLS before public access. FQDN/DNS and resources follow [upstream](https://docs.mailcow.email/getstarted/prerequisite-system/); LXC/OpenVZ are unsupported upstream.

## License

[MIT](LICENSE) — `Copyright (c) 2026 OxidiLily`; retain attribution and the license notice.
