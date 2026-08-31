// Terjemahan kalimat error yang datang dari backend (API + helper daemon).
//
// Kalimat backend sering sudah berisi nilainya (nama user, path, angka), jadi
// tidak bisa dicocokkan sebagai teks tetap. Kunci di sini adalah kalimat Go-nya
// apa adanya — verb format (%s, %q, %v, %d, %w) dipakai sebagai penanda posisi
// nilai, dan pesan-error.ts memasang nilai itu kembali ke kalimat Inggrisnya.
//
// Urutan penting: kalimat yang lebih spesifik ditaruh lebih dulu, karena pola
// pertama yang cocok yang dipakai.
const kalimatBackend: Record<string, string> = {
  // ---- validasi & izin (internal/helper) ----
  "aksi firewall harus allow atau deny": "firewall action must be allow or deny",
  "aksi service tidak diizinkan": "service action not allowed",
  "aksi tidak dikenal": "unknown action",
  "akun ini tidak punya shell login": "this account has no login shell",
  "akun root tidak boleh dihapus": "the root account cannot be deleted",
  "akun root tidak boleh dikunci dari panel": "the root account cannot be locked from the panel",
  "akun service tidak boleh login": "service accounts are not allowed to log in",
  "alamat DNS tidak valid: %s": "invalid DNS address: %s",
  "alamat IP tidak valid": "invalid IP address",
  "alamat sumber tidak valid": "invalid source address",
  "argumen docker kosong": "empty docker arguments",
  "argumen kosong untuk %s": "empty argument for %s",
  "argumen tidak valid: %v": "invalid argument: %v",
  "cloudflared belum terpasang — install dulu lewat Components":
    "cloudflared is not installed — install it from Components first",
  "command file tidak dikenal: %s": "unknown file command: %s",
  "mirror APT menolak mengirim berkas paket — repositori bisa dibaca tapi unduhannya gagal. Ganti mirror di /etc/apt/sources.list.d/ubuntu.sources ke mirror lain, lalu coba lagi. Pesan asli apt: %s":
    "The APT mirror refused to send the package file — the repository is readable but the download failed. Switch the mirror in /etc/apt/sources.list.d/ubuntu.sources to another one, then try again. Original apt message: %s",
  "vendor printer %q tidak dikenal": "unknown printer vendor %q",
  "CUPS belum terpasang — pasang komponen \"print-server\" dulu lewat Settings → Components":
    "CUPS is not installed — install the \"print-server\" component from Settings → Components first",
  "nama printer tidak valid — huruf/angka/titik/garis, tanpa spasi":
    "invalid printer name — letters/digits/dots/dashes, no spaces",
  "device URI tidak valid": "invalid device URI",
  "device URI dengan skema %q tidak diizinkan": "device URI with scheme %q is not allowed",
  "model/driver tidak valid": "invalid model/driver",
  "deskripsi/lokasi tidak boleh mengandung baris baru": "description/location must not contain newlines",
  "id job tidak valid": "invalid job id",
  "jumlah salinan harus 1–100": "copies must be between 1 and 100",
  "opsi cetak tidak valid": "invalid print option",
  "cetak gagal: %s": "printing failed: %s",
  "command tidak dikenal: %s": "unknown command: %s",
  "component %q tidak dikenal": "unknown component %q",
  "component %s tidak punya service": "component %s has no service",
  "config %s belum ada": "config %s does not exist yet",
  "config WireGuard harus punya section [Interface]": "the WireGuard config must have an [Interface] section",
  "Docker belum terpasang — pasang dulu lewat Settings → Components":
    "Docker is not installed — install it from Settings → Components first",
  "dir harus absolut": "dir must be absolute",
  "durasi %q tidak valid — pakai format seperti 10m, 1h, 1d":
    "invalid duration %q — use a format like 10m, 1h, 1d",
  "exportfs menolak konfigurasi: %v": "exportfs rejected the configuration: %v",
  "export %s didefinisikan di luar panel — hapus barisnya sendiri di /etc/exports":
    "export %s is defined outside the panel — remove its line in /etc/exports yourself",
  "fail2ban menolak konfigurasi: %v": "fail2ban rejected the configuration: %v",
  "folder sumber %q tidak valid (harus path absolut tanpa spasi/titik dua)":
    "invalid source folder %q (must be an absolute path without spaces or colons)",
  "folder sumber tidak boleh sama dengan mount point": "a source folder cannot be the mount point itself",
  "grup %q tidak ada": "group %q does not exist",
  "grup %q tidak ditemukan": "group %q not found",
  "home directory %q tidak valid untuk user %s": "invalid home directory %q for user %s",
  "home directory tidak valid untuk user %s": "invalid home directory for user %s",
  "home %s masih milik user %s — pindahkan atau hapus dulu":
    "home %s still belongs to user %s — move or delete it first",
  "hostname tidak valid": "invalid hostname",
  "identitas tidak dikenal": "unknown identity",
  "interface default tidak ditemukan": "default interface not found",
  "jail %q didefinisikan di luar panel — ubah filenya sendiri":
    "jail %q is defined outside the panel — edit its file directly",
  "klien %q tidak valid": "invalid client %q",
  "konfigurasi Samba ditolak: %v": "the Samba configuration was rejected: %v",
  "maxretry harus 1–100": "maxretry must be 1–100",
  "minimal satu klien harus diisi": "at least one client is required",
  "minimal satu nameserver": "at least one nameserver is required",
  "mesin ini bukan guest QEMU/KVM — perangkat %s tidak ada, jadi qemu-guest-agent tidak bisa dijalankan. Agent ini hanya berguna di VM Proxmox/QEMU":
    "this machine is not a QEMU/KVM guest — the device %s does not exist, so qemu-guest-agent cannot start. This agent is only useful inside a Proxmox/QEMU VM",
  "mode tidak valid": "invalid mode",
  "mount point harus path absolut, bukan /": "the mount point must be an absolute path, not /",
  "mount point %s sudah didefinisikan di fstab di luar panel":
    "mount point %s is already defined in fstab outside the panel",
  "mount point tidak valid": "invalid mount point",
  "mount pool gagal: %s": "mounting the pool failed: %s",
  "nama jail tidak valid": "invalid jail name",
  "nama service tidak valid": "invalid service name",
  "nama share tidak valid": "invalid share name",
  "nomor rule tidak valid": "invalid rule number",
  "opsi mount mengandung karakter yang tidak diizinkan": "the mount options contain disallowed characters",
  "opsi %q mengandung karakter yang tidak diizinkan": "option %q contains disallowed characters",
  "opsi %s butuh nilai": "option %s needs a value",
  "password baru wajib diisi": "the new password is required",
  "password lama salah": "the current password is wrong",
  "password lama wajib diisi": "the current password is required",
  "path export harus absolut dan tanpa spasi": "the export path must be absolute and without spaces",
  "path share harus absolut": "the share path must be absolute",
  "path %s sudah didefinisikan di /etc/exports di luar panel":
    "path %s is already defined in /etc/exports outside the panel",
  "PID tidak valid": "invalid PID",
  "pool butuh minimal dua folder sumber": "a pool needs at least two source folders",
  "pool %s didefinisikan di luar panel — hapus barisnya sendiri di /etc/fstab":
    "pool %s is defined outside the panel — remove its line in /etc/fstab yourself",
  "port %q tidak valid": "invalid port %q",
  "port tidak valid": "invalid port",
  "protokol tidak valid": "invalid protocol",
  "set password Samba gagal: %s": "setting the Samba password failed: %s",
  "share %q sudah didefinisikan di smb.conf di luar panel — hapus definisi itu dulu kalau ingin dikelola dari sini":
    "share %q is already defined in smb.conf outside the panel — remove that definition first if you want to manage it from here",
  "shell %q tidak terdaftar di /etc/shells": "shell %q is not listed in /etc/shells",
  "signal tidak diizinkan": "signal not allowed",
  "smb.conf tidak ditemukan — pasang paket samba dulu": "smb.conf not found — install the samba package first",
  "spec rule kosong": "empty rule spec",
  "spec rule tidak valid": "invalid rule spec",
  "subcommand compose %q tidak diizinkan": "compose subcommand %q is not allowed",
  "subcommand compose tidak ditemukan": "compose subcommand not found",
  "subcommand docker %q tidak diizinkan": "docker subcommand %q is not allowed",
  "Tailscale belum terpasang — install dulu lewat Components":
    "Tailscale is not installed — install it from Components first",
  "tidak bisa melepas %s: %v — pastikan tidak ada file yang sedang dipakai":
    "cannot unmount %s: %v — make sure no file is still in use",
  "username atau password salah": "wrong username or password",
  "username dan password wajib diisi": "username and password are required",
  "username Samba tidak valid": "invalid Samba username",
  "username tidak valid (huruf kecil, angka, - dan _)":
    "invalid username (lowercase letters, digits, - and _)",
  "username tidak valid": "invalid username",
  "user %q sudah ada": "user %q already exists",
  "user %q tidak ditemukan: %w": "user %q not found: %w",
  "user %q tidak ditemukan": "user %q not found",
  "VPN %q tidak dikenal": "unknown VPN %q",
  "WireGuard belum terpasang — install dulu lewat Components":
    "WireGuard is not installed — install it from Components first",

  // ---- handler HTTP (internal/api) ----
  "Alamat DNS tidak valid: %s": "Invalid DNS address: %s",
  "aksi container tidak dikenal": "unknown container action",
  "aksi stack tidak dikenal": "unknown stack action",
  "ambang tidak valid: warn harus < crit, keduanya 1-100":
    "invalid thresholds: warn must be < crit, both 1-100",
  "bahasa harus id atau en": "language must be id or en",
  "body request tidak valid: %s": "invalid request body: %s",
  "bookmark tidak ditemukan": "bookmark not found",
  "compose ditolak Docker: %s": "Docker rejected the compose file: %s",
  "container sudah berjalan": "the container is already running",
  "endpoint tidak ditemukan": "endpoint not found",
  "File ini bukan teks — tidak bisa diedit": "This file is not text — it cannot be edited",
  "File lebih dari 1 MB — terlalu besar untuk diedit di browser":
    "The file is larger than 1 MB — too big to edit in the browser",
  "gagal membaca daftar proses: %s": "failed to read the process list: %s",
  "gagal membaca part: %s": "failed to read the upload part: %s",
  "gagal membuat session: %s": "failed to create the session: %s",
  "gagal menulis .env: %s": "failed to write .env: %s",
  "gagal menulis file sementara: %s": "failed to write the temporary file: %s",
  "gagal menulis file": "failed to write the file",
  "helper daemon tidak dapat dihubungi: %w": "the helper daemon could not be reached: %w",
  "id container kosong": "empty container id",
  "id tidak valid": "invalid id",
  "interval polling harus antara 250ms dan 60000ms": "the polling interval must be between 250ms and 60000ms",
  "isi compose tidak boleh kosong": "the compose content cannot be empty",
  "Isi melebihi 1 MB": "The content exceeds 1 MB",
  "Layanan autentikasi tidak tersedia. Cek status linux-dashboard-helper.service.":
    "The authentication service is unavailable. Check linux-dashboard-helper.service.",
  "metrik %q tidak punya alert threshold": "metric %q has no alert threshold",
  "Nama stack wajib diisi": "The stack name is required",
  "op tidak dikenal: %s": "unknown op: %s",
  "parameter path wajib diisi": "the path parameter is required",
  "Password baru minimal 8 karakter": "The new password must be at least 8 characters",
  "Password minimal 8 karakter": "The password must be at least 8 characters",
  "path harus absolut": "the path must be absolute",
  "Path docker-compose.yml harus absolut": "The docker-compose.yml path must be absolute",
  "Permintaan lintas situs ditolak": "Cross-site request denied",
  "request bukan multipart: %s": "the request is not multipart: %s",
  "response helper tidak valid: %w": "invalid helper response: %w",
  "service tidak ditemukan": "service not found",
  "Sesi tidak valid atau sudah berakhir": "The session is invalid or has expired",
  "stack tidak ditemukan": "stack not found",
  "style PAM tidak didukung": "unsupported PAM style",
  "tampilan file manager harus grid atau list": "the file manager view must be grid or list",
  "Tidak bisa menghapus akun yang sedang dipakai login":
    "Cannot delete the account currently used to log in",
  "unduh %s gagal: HTTP %d": "downloading %s failed: HTTP %d",
  "upload gagal: %s": "upload failed: %s",
  "Username atau password salah": "Wrong username or password",
  "worker gagal: %w": "the worker failed: %w",
  "worker tidak mengirim hasil: %w": "the worker returned no result: %w",
  "zona waktu %q tidak dikenal": "unknown time zone %q",

  // Paling umum — ditaruh terakhir supaya tidak menelan kalimat yang lebih
  // spesifik di atasnya.
  "%s adalah direktori": "%s is a directory",
}

// Verb format Go dijadikan grup tangkapan supaya nilai yang sudah disisipkan
// backend (nama, path, angka) bisa dipindahkan ke posisi yang sama di kalimat
// bahasa Inggrisnya.
const pola = Object.entries(kalimatBackend).map(([id, en]) => ({
  re: new RegExp(
    "^" +
      id
        .replace(/[.*+?^${}()|[\]\\]/g, "\\$&")
        .replace(/%[sqvdw]/g, "([\\s\\S]*?)") +
      "$",
  ),
  en,
}))

/**
 * Terjemahkan satu kalimat error dari backend ke bahasa Inggris.
 * Kalimat yang tidak dikenali dikembalikan apa adanya — lebih baik menampilkan
 * kalimat Indonesia daripada menyembunyikan penyebab kegagalan.
 */
export function pesanBackendEn(pesan: string): string {
  for (const { re, en } of pola) {
    const cocok = pesan.match(re)
    if (!cocok) continue
    let i = 1
    return en.replace(/%[sqvdw]/g, () => cocok[i++] ?? "")
  }
  return pesan
}
