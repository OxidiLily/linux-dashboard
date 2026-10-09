[Bahasa Indonesia](README.md) · **English**

# Linux Server Dashboard
A Go + React panel for one Ubuntu/Debian server: homelab, NAS, or small server.

## Panel features

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

## Strengths

- Integrated responsive ID/EN UI; existing Linux accounts through PAM.
- Files/terminals follow account permissions. Non-root web process, separate root helper over Unix socket + HMAC; admin actions require root or `sudo`/`admin` membership.
- Install components as needed; existing software is detected, pages guarded when dependencies are missing.
- Terminal/AI sessions survive page changes, not browser reloads.

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

## Operations & development

- Panel updates compare file changes, not only commit SHA: README/root Markdown and `docs/` do not trigger updates; code, dependencies, installer and runtime assets still do. Fetch/comparison failures retain a conservative status.
- Config: `/etc/default/linux-dashboard`; units `linux-dashboard-web.service` and `linux-dashboard-helper.service`.
- HTTPS reverse proxy: keep the HTTP panel upstream on `http`; set `DASHBOARD_SECURE_COOKIE=true`. Existing native cert/key pairs still enable TLS.
- Supabase declares ports 8000/5432/6543 for firewall rules; restrict DB access. Applying `.env` changes needs **Up**, not Restart.
- mailcow requires a mail FQDN/DNS and resources matching [upstream](https://docs.mailcow.email/getstarted/prerequisite-system/); LXC/OpenVZ are unsupported upstream.
- AI needs appropriate logins/providers; Hermes/OpenClaw can bootstrap to 9router when an active key exists. Per-account state: `~/DATA/AppData/linux-dashboard`.
- Build: Go 1.26.6+, Node.js 24.15+ or >=26, a C compiler, `make`, `libpam0g-dev`. Embedded UI changes require rebuilding/deployment to go live.

```bash
make build
make test
make lint
python3 -B -m unittest discover -s deploy -p 'test_*.py'
```

Some tests skip without root/packages/hardware; local gates do not prove deployment/E2E.

[MIT](LICENSE) — `Copyright (c) 2026 OxidiLily`; retain the license notice.
