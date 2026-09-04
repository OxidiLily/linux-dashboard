[Bahasa Indonesia](README.md) · **English**

# Linux Server Dashboard (Go + React)

A web panel for monitoring and managing a single Linux server: real-time
metrics, file manager, file sharing (Samba/NFS/mergerfs), a print server (CUPS),
processes, Docker, terminal, firewall, and system settings. Login uses the Linux accounts already
present on the machine — there is no separate user table.

Target: **Ubuntu & Debian**, architectures **amd64 / arm64 / armhf**, designed to
stay light on a **2-core** machine.

> **Disclaimer:** this project is still under development, so apologies for any
> bugs you run into.

## Quick install

```bash
curl -fsSL https://raw.githubusercontent.com/OxidiLily/linux-dashboard/main/deploy/install.sh | sudo bash
```

The script installs the build dependencies (Go, Node 24, `libpam0g-dev`), fetches
the sources into `/usr/local/src/go-react-linux-dashboard`, builds the UI plus the
two binaries, installs the systemd units and the PAM file, then starts the service
on port **1122**. Running the same command again upgrades to the latest `main`.

The installer **detects first, then installs**: dependencies that are already
present are skipped, and a Go installed outside apt (official tarball, asdf,
snap) is not replaced by the `golang-go` package. At the end it reports which
optional components already exist and which ones you still need to install from
the Components menu.

Login uses the Linux accounts already on the machine (an account in the `sudo`
group is required for the Docker, Firewall, Fail2ban, Samba, Disk Pool, NFS, and
Components menus).

---

## Menus

| Group | Menu |
|---|---|
| Home | Dashboard (CPU, RAM, Storage, GPU, Network in real time; empty disks can be formatted & mounted from here) |
| File manager | File Manager (text editor, file creation, printing) · Samba (shares + users) · Disk Pool (mergerfs) · NFS Exports · Bookmarks |
| AI | AI Agent (agent CLI sessions inside the panel: claude-code, codex, opencode, hermes, openclaw) |
| Logs | Logs (every panel alert) · File Operations · Activity Logs |
| Settings | Network (DNS + Tailscale/Cloudflare Tunnel/WireGuard) · Firewall (ufw) · Fail2ban · Alert Thresholds · Print server (CUPS) · Components |
| System | Processes · Docker (per-container actions, logs, compose & `.env` editors) · Terminal |

**Account** is not in the sidebar: its entry point is the profile block at the
foot of the sidebar, which opens a menu holding the account identity, Account,
Uninstall panel (sudoers only), and Sign out. The route is still
`/settings/account`.

Pages that need specific software (Samba, ufw, Docker, mergerfs, NFS, fail2ban)
show **"Not Installed"** with a button to Components — not an empty list or a raw
`command not found` error.

The **Logs** menu holds three views whose retention the server actually
enforces rather than merely promising: **Logs** (every panel alert — success,
failure, warning, info — filterable by status, **1 month**), **File
Operations** (**1 month**), and **Activity Logs** (the login & admin-action
audit trail, **2 years**). Anything past its age is deleted by a sweep that
runs at server start and once an hour after that. The Logs page records the
notifications that actually appeared on screen, so failures that never reached
the server — browser-side validation, a dropped connection — still leave a
trace, together with the page they came from and their raw output.

The panel is fully usable on a **phone screen**: below `lg` the sidebar becomes a
drawer with a scrim (dismissed by the scrim, Escape, or picking a menu item), and
below 640px tables turn into stacks of cards through a single CSS class plus a
`data-label` per cell — the `<table>` structure stays the only source, so column
order and screen-reader output never grow a divergent twin. Rename, Edit text,
and Change Permission in the File Manager have per-row buttons on a phone,
because a right-click menu cannot be relied on with touch.

The interface is fully available in **Indonesian and English** — including error
messages coming from the backend. The language and time zone pickers live in the
topbar and are stored per account on the server, not in the browser.

## Components

32 optional pieces of software that are not part of a base Ubuntu/Debian
install, installable and removable from the panel:

| Category | Contents |
|---|---|
| Runtime & tunnel | docker · nodejs · tailscale · cloudflared · wireguard |
| AI & Agent | 9router · hermes · claude-code · codex · opencode · openclaw · rtk · graphify · ponytail · browser-use |
| Database & backend | supabase |
| File sharing & network | samba · nfs-server · cifs-utils · avahi · technitium-dns · print-server · mergerfs |
| Security | ufw · fail2ban |
| Monitoring & disk | lm-sensors · smartmontools · nvme-cli · qemu-guest-agent |
| Utilities | htop · ncdu · fastfetch · restic |

Software already present on the system — installed by hand or from another
repository — is recognized as-is and **never reinstalled**.

While an install runs, the component card shows a **percentage bar** whose
numbers come from apt itself (`APT::Status-Fd`), not from a stopwatch: index
0–10%, download 10–55%, install 55–99%, and the number never goes backwards.
Vendor install scripts are read too: the apt they invoke writes to the same fd
via `APT_CONFIG`, so Tailscale gets real numbers as well. Until the first
report arrives — npm installers, or a script still fetching its own files — a
small filled segment travels across the track and the caption names the step
being run ("downloading and installing the npm package"). A track filled edge
to edge is deliberately avoided: it reads as work stuck at 100%. While the
action runs the card's badge reads "Installing", never "Not Installed" sitting
next to a bar that is clearly moving.

**Removing a component** offers a "delete its data too" checkbox, but only for
components that actually store something outside their package (flagged
`has_data` by the helper). It defaults to off, because what it deletes cannot be
brought back. A concrete use: `~/.9router` holds a password the user has already
changed, and while that folder exists, reinstalling will never restore the
initial password.

`cloudflared` is the one component with **no** start/stop button on this page:
its tunnel means nothing without a token, and the token is entered in Settings →
Network — so control lives there and Components only shows the status. Removing
it also deletes the `cloudflared.service` systemd unit written by
`cloudflared service install <token>`; the tunnel token lives inside that unit
and is not part of the .deb, so without this step the old tunnel key survives
the uninstall.

The panel uninstaller's **"Full removal"** mode now removes every component the
panel can install — Docker, Node.js, Tailscale, cloudflared and the AI tools
included — along with their data, using the same uninstallers as the Components
page. Docker images and volumes in `/var/lib/docker` are still left alone: they
belong to your containers, not to the panel.

### Self-hosted Supabase

The `supabase` component installs a full Supabase backend — Postgres, Auth
(GoTrue), PostgREST, Realtime, Storage, Edge Functions, and Studio — as a
Docker Compose stack in `/opt/supabase/supabase-project`. What the panel runs
is the **official setup.sh** (`curl -fsSL https://supabase.link/setup.sh | sh`;
here the script is downloaded to a file first, then executed as
`sh setup.sh -y`), following
<https://supabase.com/docs/guides/self-hosting/docker>. That script sparse-
clones the `docker/` folder from the latest self-hosted release tag and
generates every secret through `utils/generate-keys.sh` and
`utils/add-new-auth-keys.sh` — JWT_SECRET, ANON_KEY, SERVICE_ROLE_KEY,
POSTGRES_PASSWORD, and DASHBOARD_PASSWORD. The panel never writes the compose
file or the keys itself: key generation is the part most easily left behind
when Supabase ships a new release, and getting it wrong leaves the deployment
open to anyone.

Three things the panel does around it:

1. **Docker is installed through the panel's own path**, not left to setup.sh —
   so the account that pressed Install is added to the `docker` group and
   System → Docker can actually manage the new stack.
2. **Public URLs are pointed at this machine's LAN IP.** The `.env.example`
   default is `http://localhost:8000`, and that value is what the BROWSER uses
   to call the API — left alone, Studio only works from the server itself.
   Only `SUPABASE_PUBLIC_URL` and `API_EXTERNAL_URL` are rewritten; `SITE_URL`
   points at your own application, not at Supabase.
3. **The stack is started once** with `sh run.sh start --wait-timeout 600`
   (Supabase's official wrapper around `docker compose up -d --wait`), so once
   Install finishes the stack is already visible and manageable in
   System → Docker.

Only port **8000** (the Kong/Envoy gateway — Studio, REST, Auth, Realtime, and
Storage all sit behind it) is registered with ufw. Postgres 5432 and the pooler
on 6543 are published by the default compose too, but opening them to the whole
LAN is an admin decision in Settings → Firewall, not a side effect of pressing
Install. The **Open** button on the component card points at
`http://<panel host>:8000`; Studio credentials live in
`DASHBOARD_USERNAME`/`DASHBOARD_PASSWORD` and can be read through the stack
`.env` editor in System → Docker.

**Removing it does not delete the database.** All Supabase data lives inside
the project folder (`volumes/db/data`, `volumes/storage`, and the `.env` that
holds JWT_SECRET), so a plain uninstall stops the stack and *moves* the folder
to `/opt/supabase/bekas-<date>-<time>` — the card goes back to "not installed",
the next install is not rejected by setup.sh, and the data is still there if it
turns out to be needed. Tick "delete data too" to remove `/opt/supabase`
entirely, including any `bekas-*` folders.

### Required AI Agent tools & skills

The last four components in the AI category — `rtk`, `graphify`, `ponytail`,
`browser-use` — are not agents but tooling used by **every** agent. They are installed
automatically as soon as any agent is installed, and their usage directives are
written into each agent's global instruction file (`~/.claude/CLAUDE.md`,
`~/.codex/AGENTS.md`, and so on) whenever an AI Agent session is opened — so
panel accounts created after installation get them too.

| Tool | Role | Docs |
|---|---|---|
| rtk | Trims shell command output before it reaches the agent context | <https://github.com/rtk-ai/rtk#quick-start> |
| graphify | Code knowledge graph via local AST parsing | <https://github.com/Graphify-Labs/graphify#install> |
| ponytail | "Lazy senior dev" harness at ultra level + audit/review/debt skills | <https://github.com/DietrichGebert/ponytail#install> |
| browser-use | CDP browser control — JavaScript-rendered pages, logins, clicks, forms | <https://browser-use.com> · <https://docs.browser-use.com> |

Registration happens **per user and per agent**, right before an AI Agent
session opens — not once at install time. The helper daemon runs as root, so a
`rtk init -g` called by the installer would only patch `/root`; other panel
accounts open agents with their own HOME. Targets used per agent:

| Panel agent | rtk | graphify | browser-use |
|---|---|---|---|
| claude-code | `rtk init -g --auto-patch --no-trust-filters` | `graphify install --platform claude` | `--target claude` |
| codex | `rtk init -g --codex` | `graphify install --platform codex` | `--target codex` |
| opencode | `rtk init -g --opencode --auto-patch --no-trust-filters` | `graphify install --platform opencode` | `--target opencode` |
| hermes | `rtk init -g --agent hermes` | `graphify install --platform hermes` | — (no skill directory yet) |
| openclaw | — (rtk has no OpenClaw target yet) | `graphify install --platform claw` | `--target openclaw` |

`--auto-patch` and `--no-trust-filters` are required for targets that patch
`settings.json`: without them rtk prompts on the terminal, and the daemon has
nobody to answer.

The browser-use column is the argument to `browser-use skill install
--no-install --target <value>`, which writes the official `SKILL.md` into that
agent's skill directory. `--no-install` is required: without it the command
installs a second copy of browser-use through `uv` into `~/.local/bin`, racing
the system-wide one the panel already installed. hermes has no skill directory
in browser-use's list, so for it only the directives in `~/.hermes/AGENTS.md`
apply.

## Print server (CUPS)

`print-server` is an **optional** component and deliberately not part of the base
install: this panel runs on every kind of machine, and inside LXC a print server
cannot work at all. A machine that will never print is therefore not considered
incompletely installed — the Print server page simply shows "Not Installed" with
a button to Components.

Once installed, the whole printing flow finishes inside the panel, without
opening a terminal:

1. **Components → print-server** installs `cups` + `printer-driver-gutenprint`.
   Gutenprint comes along as part of the component, not as an option: home USB
   printers (Canon PIXMA, Epson, many HPs) do not support IPP Everywhere, and
   CUPS will happily accept a driverless queue right up until every print job
   comes out blank.
2. **Settings → Print server → Detect** joins the three things that have to be
   seen together — devices from `lpinfo`, the drivers present on the system, and
   the queues that already exist — so "printer ready to register" can be told
   apart from "printer visible but its driver is missing".
3. **Install the driver** for printers that are not ready yet. What the frontend
   sends is a **vendor** name, not a package name; the vendor → package mapping
   is a backend whitelist (`internal/helper/printer.go`), so this endpoint can
   never turn into "the frontend picks any package to install as root".
4. **Register the queue**, then **print** from the File Manager menu (pick the
   printer, copies, media size, one- or two-sided) and watch the queue.

The sudo split on this page is deliberately uneven: changing the printer list,
scanning for devices, and installing drivers need sudo because they change the
machine's configuration for everyone. Listing printers, watching the queue, and
printing your own file do not — that is the very reason the feature exists.

Printing does **not** open the file as root: after the path check, its contents
are streamed through a worker already dropped to the logged-in user's privileges
and into `lp`'s stdin. The `file://` device scheme is rejected — that is not a
printer, that is a way to make `cupsd` write arbitrary files as root.

Network printer discovery over mDNS depends on Avahi. Avahi is deliberately
**not** a dependency of print-server (it changes the machine's network behaviour
far more broadly than printing does), so the Detect page spells out that
condition and its way out when Avahi is missing.

---

## Firewall: component ports are registered before the firewall comes up

`ufw` is installed with `DEFAULT_INPUT_POLICY=DROP`, so enabling it cuts off
every service whose port has not been allowed. Ports are therefore **declared per
component** and registered when that component is installed, not when the
firewall is switched on — `ufw allow` is stored in `/etc/ufw/user.rules` even
while ufw is inactive:

| Component | Ports |
|---|---|
| samba | 445/tcp · 139/tcp · 137:138/udp |
| nfs-server | 2049/tcp · 111/tcp · 111/udp |
| avahi | 5353/udp |
| print-server | 631/tcp |
| 9router | 20128/tcp |

The source is limited to the local subnet detected from the default route; the
SSH port and the panel port are deliberately left `Anywhere`, because an admin
may come in from another subnet. Installing `ufw` later does not leave existing
components behind — at that point every installed component's ports are
registered after the fact. Removing a component withdraws its allowance again.

fail2ban ships no filter for a single component in the catalog, so the `sshd`
jail is enabled automatically when fail2ban is installed, and the Samba filter is
installed by the panel itself. That filter is only useful if failed logins are
actually logged, while Ubuntu's default `map to guest = Bad User` maps unknown
usernames to guest without a single `NT_STATUS_LOGON_FAILURE` line — so a block
holding `map to guest = Never` and `log level = 0 auth_audit:3` is appended at the
**end** of `smb.conf`'s `[global]` section (Samba uses the last value within a
section, so the panel's setting wins without editing the admin's lines, and
removing the block restores the old configuration exactly).

Rules and jails created afterwards **belong to the user**: `ufw enable` does not
re-register component ports, and a jail that already exists or has been deleted
is not recreated. The only thing still guaranteed before the firewall comes up is
admin access, because losing that means losing the machine.

---

## Disks & Disk Pool

Raw, unused disks are reported by the collector (`unused_disks`: no partitions,
no LVM/RAID holder, not mounted) and appear on the dashboard's Storage card.
Their capacity deliberately does **not** count towards total storage — that space
is not usable yet, so including it would make the usage percentage lie.

Clicking such a disk opens the format & mount dialog: pick a mount point and a
filesystem (ext4/xfs/btrfs), and the helper runs `mkfs`, writes an `/etc/fstab`
entry by UUID with `nofail`, and mounts it. The guards: only disks that
`UnusedDisks()` acknowledges may be touched (exactly the list the dashboard
uses), a disk that turns out to already hold a filesystem is refused with the
code `disk_has_filesystem` and the dialog then offers mounting without
formatting, and `fstab` is written atomically and rolled back if the mount fails.

**Disk Pool (mergerfs)** merges several disks into one mount point. What matters:

- The default policy is `category.create=pfrd` — new files are spread randomly
  weighted by free space, not piled onto one disk the way `mfs` does. Existing
  pools are not changed; their options live in `/etc/fstab` and only change
  through Edit.
- A pool can be **mounted/unmounted** without deleting its definition. The
  operation is idempotent and never touches `/etc/fstab`, so an unmounted pool
  comes back after a reboot.
- The mount point is **locked immutable** whenever the directory is bare (just
  before mounting and after unmounting). Without that, files uploaded while the
  pool is down fill the system disk and then vanish from view once the pool is
  mounted again.
- Unmounting or deleting a pool also removes its mount point directory when it is
  empty; a directory that is **not** empty is kept with its contents and locked.
- Every mounted pool shows up as a "Disk pool : &lt;Name&gt;" shortcut right after
  `Root (/)` in the File Manager — sudo-only, just like `Root (/)`.

---

## Configuration owned by the system

Samba shares, mergerfs pools, disks prepared by the panel, NFS exports, fail2ban
jails, and printer queues are written into system configuration files
(`smb.conf` include, `/etc/fstab`, `/etc/exports`, `jail.local`, `printers.conf`
via `lpadmin`). The same rules apply to all of them:

- lines/sections **not written by the panel** are still displayed, marked, and
  read-only — configuration owned by the admin is never rewritten;
- writes go through a temporary file followed by `rename`, because a file
  truncated mid-write can leave the system unbootable;
- the status shown is read from the system (`exportfs -s`, `findmnt`,
  `fail2ban-client status`, `lpstat`), not from the file contents.

One deliberate exception: the `[global]` block the panel appends to `smb.conf` so
that failed Samba logins are actually recorded (see the Firewall section above).
It is appended at the end of the section rather than overwriting the admin's
lines; the original is backed up to `smb.conf.lindash.bak`, and a result that
`testparm` rejects is restored automatically before `smbd` gets a chance to fail
to start.

---

## Architecture

Two processes, split by privilege:

```
Browser (React SPA)
    │ HTTPS / WebSocket
    ▼
linux-dashboard-server      ← non-root user (linux-dashboard)
  REST API · WebSocket hub · embedded SPA · SQLite
    │ Unix socket + HMAC
    ▼
linux-dashboard-helper      ← root
  PAM auth · file ops (fork+setuid) · systemctl · ufw · samba ·
  useradd · apt · docker · PTY
```

The web process **never** has root access. Every privileged operation is sent to
the helper daemon as a structured command, signed with HMAC, and executed with an
argument array — never through `sh -c`.

For operations that must run **as the logged-in user** (reading/writing files in
the home directory, killing your own processes, the terminal shell), the helper
forks a child process with `SysProcAttr.Credential`. The kernel enforces the
permissions, not our code — so no Unix permission logic is reimplemented and no
such reimplementation can be wrong.

## Layout

```
cmd/server          web app entrypoint
cmd/helper          helper daemon entrypoint (+ worker mode)
internal/helperproto  the command contract between the two
internal/helper       root daemon implementation (components, samba, mergerfs,
                      nfs, fail2ban, printer, portkomponen, progres, vpn,
                      files, users, docker, terminal)
internal/helperclient HMAC client for the daemon
internal/api          REST handlers + WebSocket
internal/metrics      gopsutil collector + multi-vendor GPU detection
internal/platform     OS/kernel/platform detection (14 scenarios)
internal/store        SQLite: sessions, logs, bookmarks, thresholds, stacks
internal/terminal     terminal session quota + live session list, based on the core count
internal/config       configuration from the environment
web/embed.go          go:embed of the React build output
web/ui                frontend sources (React TSX + Vite + Tailwind v4)
deploy/               systemd units, PAM file, one-line installer
```

## Building

Requires **Go 1.26.6+** (the version in `go.mod`; older toolchains download the
right one automatically through `GOTOOLCHAIN=auto`), **Node.js 20+**, and
`libpam0g-dev` — the helper daemon uses PAM through cgo.

```bash
sudo apt install -y build-essential libpam0g-dev
make build          # build UI → embed → two binaries in bin/
sudo ./deploy/install.sh
```

Dropping the `sudo` also works: the script notices it is not root and re-runs
itself through `sudo` (override variables such as `PREFIX` are carried over). If
the `sudo` package itself is missing, the installer installs it — the `sudo`
group that package creates is what the panel uses to decide who is a sudoer. The
piped `curl` form still has to be written as `| sudo bash`, since there is no
file to re-execute.

### Dependencies

**Build** (installed automatically by the installer when missing):
`ca-certificates`, `curl`, `git`, `make`, `build-essential`, `libpam0g-dev`,
Go 1.26.6+, Node 24 from NodeSource (an existing Node 20+ is left alone).

**Runtime, from the base system** — used by the helper daemon and already present
on a normal Ubuntu/Debian; the installer warns when a minimal image has stripped
them: `systemctl`, `ip`, `hostnamectl`, `resolvectl`, `findmnt`, `mount`/`umount`,
`useradd`/`usermod`/`userdel`, `apt-get`, `dpkg-query`.

**Optional runtime** — not installed by the installer, managed from the Components
menu; pages that need them show "Not Installed" until they are present:

| Package | Binary | Page |
|---|---|---|
| samba | `smbd`, `smbpasswd`, `pdbedit`, `testparm` | File manager → Samba |
| mergerfs | `mergerfs` (needs `/dev/fuse`) | File manager → Disk Pool |
| cups + printer-driver-gutenprint | `cupsd`, `lpadmin`, `lpinfo`, `lpstat`, `lp` | Settings → Print server |
| nfs-kernel-server | `exportfs` | File manager → NFS Exports |
| ufw | `ufw` | Settings → Firewall |
| fail2ban | `fail2ban-client` | Settings → Fail2ban |
| docker-ce + docker-compose-plugin (official Docker repo) | `docker` | System → Docker |
| wireguard, tailscale, cloudflared | `wg`/`wg-quick`, `tailscale`, `cloudflared` | Settings → Network |
| nodejs | `node`, `npm` | Components → 9Router |

**Go libraries**: `go-chi/chi/v5`, `coder/websocket`, `creack/pty`,
`msteinert/pam/v2` (cgo), `shirou/gopsutil/v4`, `modernc.org/sqlite` (pure Go).
**Frontend**: React 18 + Vite 6 + TypeScript 5.7, Tailwind v4 (`@tailwindcss/vite`),
`@radix-ui/react-slot`, Zustand 5, react-router-dom 6, `@xterm/xterm` 5,
`lucide-react`. Dialogs, toasts, and the other UI components are the project's
own TSX source in `src/components/ui/` — not a third-party library.

`make release-server` builds the web app for amd64, arm64, and armhf in one go
(Go's built-in cross-compilation, no extra toolchain — the web app is
deliberately `CGO_ENABLED=0`). The helper daemon uses PAM through cgo, so it must
be built with a compiler for its target architecture.

## Development

```bash
sudo go run ./cmd/helper       # terminal 1
go run ./cmd/server            # terminal 2
cd web/ui && npm run dev       # terminal 3 → http://localhost:5173
```

Vite proxies `/api` and `/ws` to `127.0.0.1:1122`.

## Configuration

Everything is set through environment variables; the values below are the
defaults.

| Variable | Default | Description |
|---|---|---|
| `DASHBOARD_LISTEN` | `0.0.0.0:1122` | Web app bind address |
| `DASHBOARD_TLS_CERT` | empty | TLS certificate; leave empty when behind a reverse proxy |
| `DASHBOARD_TLS_KEY` | empty | TLS private key; must be set together with `DASHBOARD_TLS_CERT` |
| `DASHBOARD_RUN_DIR` | `/run/linux-dashboard` | Location of the helper socket |
| `DASHBOARD_STATE_DIR` | `/var/lib/linux-dashboard` | Location of SQLite + `secret.key` |
| `DASHBOARD_SOCKET` | `$RUN_DIR/helper.sock` | Helper socket path (full override) |
| `DASHBOARD_SOCKET_GROUP` | `linux-dashboard` | Group allowed to access the socket |
| `DASHBOARD_SECRET` | `$STATE_DIR/secret.key` | Helper HMAC secret file (0600, owned by root) |
| `DASHBOARD_DB` | `$STATE_DIR/lindash.db` | SQLite database path |
| `DASHBOARD_SESSION_TTL_HOURS` | `12` | Session lifetime |
| `DASHBOARD_SECURE_COOKIE` | `false` | Set to `true` when served over HTTPS |

## Authorization model

- **Root (UID 0)** is always allowed, and this is checked **before** group
  membership — root is never a member of the `sudo` group on Debian/Ubuntu, so
  authorization that only checks groups would wrongly reject root.
- **Members of the `sudo` group** (or `admin`) are allowed to perform privileged
  operations.
- **Ordinary users** can still: view the dashboard, manage files in their own home
  directory (including the `~/DATA/*` folders), kill their own processes,
  change their own password, and use the Terminal with their account's
  permissions.
- **The installer prepares these folders for accounts that already exist** on
  the machine and drops a skeleton into `/etc/skel`, so new accounts — created
  from the panel or with `useradd -m` in a terminal — get them right away.
- `~/DATA/*` is this panel's primary data location. The default Samba share
  points there through the `%U` macro (`/home/%U/DATA/Documents`), so a single
  share gives every account its own data folder.
- **Per-user data folders** (`~/DATA/AppData`, `~/DATA/Documents`,
  `~/DATA/Downloads`, `~/DATA/Gallery`, `~/DATA/Media`) are created when the
  File Manager is opened and show up there as their own roots. They live inside
  each account's home, so nothing is shared: user A never sees user B's
  `~/DATA`. Their contents are read **as the logged-in user** — entries they
  cannot open are hidden from the listing. `Root (/)` stays sudo-only.
- Denials are always explicit: HTTP 403 with the code `requires_sudo` and the
  message "This action requires sudo access" — never a silent failure.

## Security notes

- **The web terminal** is equivalent to full SSH access through a browser, limited
  purely by the Unix permissions of the logged-in account. This is a deliberate
  product decision, but it makes the helper daemon the most sensitive component in
  the system. The **Clear sessions** button in the Terminal header closes every
  session at once (other users' included), so the panel asks for the account
  password and verifies it through PAM before running it.
- **The Docker menu requires sudo.** Access to `docker.sock` is equivalent to root
  because a container can bind-mount the host filesystem.
- **The Components menu requires sudo** — installing packages changes the system
  permanently.
- **Tunnel credentials are never shown in full.** Cloudflare Tunnel tokens and
  Tailscale auth keys are sent to the browser masked
  (`eyJhIjoiZ...xxxxxxxxxxxxxxxx`); the full value never leaves the helper daemon.
  Tailscale never returns its auth key at all, so the panel only keeps the masked
  form.
- **System configuration is always written through a temporary file + `rename`,**
  and lines owned by the admin are never touched — a broken `/etc/fstab` or
  `/etc/exports` can leave the server unbootable or expose data to hosts that
  should not have it.
- Logins are limited to 5 attempts per 5 minutes per user + IP combination. PAM
  provides no brute-force protection of its own.
- **Use HTTPS in production** (a Caddy/Nginx reverse proxy, or TLS directly).

## Testing

```bash
make test     # go test ./... + npm run test --if-present
make lint     # go vet ./...
```

Some helper tests (Linux users, Samba, ufw, fail2ban, mergerfs, NFS) touch the
real system and **skip automatically** when not run as root or when the package
under test is not installed — so `make test` is safe to run on an ordinary
development machine.

Translation coverage is checked separately, from `web/ui`:

```bash
node scripts/cek-terjemahan.mjs   # UI text not wrapped in tr()/without an English counterpart
sh   scripts/cek-runtime.sh       # runs tr()/trf()/pesanError() and small view logic for real
```

## Makefile targets

| Target | Effect |
|---|---|
| `make` / `make all` | Alias for `make build` |
| `make build` | `ui` + `server` + `helper` — the order matters, the binary embeds the UI build |
| `make ui` | `npm ci` then `vite build` into `web/dist` |
| `make server` | Web app, `CGO_ENABLED=0` (cross-compilable) |
| `make helper` | Helper daemon, `CGO_ENABLED=1` (PAM through cgo) |
| `make release-server` | Web app for amd64 + arm64 + armhf at once |
| `make install` | `build` then `./deploy/install.sh` from the checkout (run with sudo) |
| `make dev` | Prints the three commands to run in separate terminals (helper, server, `vite dev`) |
| `make clean` | Remove `bin/` and `web/dist/assets` |
