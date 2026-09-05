// Package helperproto mendefinisikan kontrak antara web app (non-root) dan
// helper daemon (root) yang berkomunikasi lewat Unix domain socket.
//
// Framing: satu request per koneksi.
//
//	client → helper : "<hex-hmac-sha256> <json-request>\n"
//	helper → client : "<json-response>\n"
//
// Untuk command bertipe stream (file.read, file.write, terminal.start),
// setelah response OK dikirim, koneksi berubah jadi kanal byte mentah.
package helperproto

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// Command yang didukung helper daemon. Whitelist — daemon menolak apa pun
// yang tidak ada di tabel ini (lihat internal/helper).
const (
	CmdAuthLogin  = "auth.login"
	CmdAuthPasswd = "auth.passwd"

	CmdSysHostnameSet = "sys.hostname_set"
	CmdSysDNSSet      = "sys.dns_set"

	CmdProcKill = "proc.kill"

	CmdSvcAction = "svc.action"

	CmdUfwStatus = "ufw.status"
	CmdUfwAdd    = "ufw.add"
	CmdUfwDelete = "ufw.delete"
	CmdUfwUpdate = "ufw.update"
	CmdUfwToggle = "ufw.toggle"

	CmdFileList   = "file.list"
	CmdFileUsage  = "file.usage"
	CmdFileMkdir  = "file.mkdir"
	CmdFileRemove = "file.remove"
	CmdFileRename = "file.rename"
	CmdFileCopy   = "file.copy"
	CmdFileMove   = "file.move"
	CmdFileChmod  = "file.chmod"
	CmdFileChown  = "file.chown"
	CmdFileRead   = "file.read"  // stream: helper → client
	CmdFileWrite  = "file.write" // stream: client → helper

	CmdSambaList   = "samba.list"
	CmdSambaSave   = "samba.save"
	CmdSambaDelete = "samba.delete"

	// User Samba punya database sendiri (smbpasswd), terpisah dari akun Linux —
	// akun Linux baru TIDAK otomatis bisa dipakai login share.
	// Pool mergerfs: beberapa direktori digabung jadi satu mount point.
	CmdFail2banList   = "fail2ban.list"
	CmdFail2banSave   = "fail2ban.save"
	CmdFail2banDelete = "fail2ban.delete"
	CmdFail2banUnban  = "fail2ban.unban"

	CmdNFSList   = "nfs.list"
	CmdNFSSave   = "nfs.save"
	CmdNFSDelete = "nfs.delete"

	// Sisi KLIEN dari NFS: export milik server lain yang dipasang di mesin
	// ini. Terpisah dari perintah di atas karena berkas yang disentuh juga
	// berbeda — /etc/fstab, bukan /etc/exports.
	CmdNFSMountList     = "nfsmount.list"
	CmdNFSMountSave     = "nfsmount.save"
	CmdNFSMountDelete   = "nfsmount.delete"
	CmdNFSMountToggle   = "nfsmount.mount"
	CmdNFSMountDiscover = "nfsmount.discover"

	// Print server (CUPS). CmdPrintFile berjalan lewat jalur fileOp, bukan
	// dispatch biasa: ia butuh identitas user login untuk memeriksa path dan
	// membaca berkasnya dengan hak user itu, bukan hak root.
	CmdPrinterList    = "printer.list"
	CmdPrinterAdd     = "printer.add"
	CmdPrinterDelete  = "printer.delete"
	CmdPrinterDefault = "printer.default"
	CmdPrinterEnable  = "printer.enable"
	CmdPrinterDevices = "printer.devices"
	CmdPrinterModels  = "printer.models"
	// Deteksi menggabungkan perangkat yang terpasang dengan driver yang ada di
	// sistem, jadi UI bisa membedakan "printer siap didaftarkan" dari "printer
	// terlihat tapi drivernya belum ada".
	CmdPrinterDeteksi       = "printer.deteksi"
	CmdPrinterDriverInstall = "printer.driver.install"
	CmdPrintJobs            = "print.jobs"
	CmdPrintCancel          = "print.cancel"
	CmdPrintFile            = "print.file"

	// Disk mentah: format (opsional) lalu daftarkan di fstab dan mount.
	CmdDiskPrepare = "disk.prepare"

	CmdMergerfsList   = "mergerfs.list"
	CmdMergerfsSave   = "mergerfs.save"
	CmdMergerfsDelete = "mergerfs.delete"
	CmdMergerfsMount  = "mergerfs.mount"

	CmdSambaUserList   = "samba.user.list"
	CmdSambaUserSet    = "samba.user.set"
	CmdSambaUserDelete = "samba.user.delete"

	CmdUserList   = "user.list"
	CmdUserCreate = "user.create"
	CmdUserModify = "user.modify"
	CmdUserDelete = "user.delete"

	CmdComponentStatusAll = "component.status.all"
	CmdComponentInstall   = "component.install"
	CmdComponentUninstall = "component.uninstall"
	CmdComponentService   = "component.service" // start/stop/restart
	// Progres instalasi dibaca terpisah dari perintah installnya. Install
	// sendiri tetap sinkron seperti sebelumnya; UI memanggil ini secara
	// berkala selama menunggu, jadi kontrak install lama tidak berubah.
	CmdComponentProgress = "component.progress"

	CmdDockerExec = "docker.exec"

	CmdVPNStatus    = "vpn.status"
	CmdVPNConfigure = "vpn.configure"

	CmdTerminalStart = "terminal.start" // stream: duplex

	CmdUpdateStatus = "update.status"
	CmdUpdateStart  = "update.start"

	CmdWGServerInfo = "wg.server.info"
	CmdWGServerInit = "wg.server.init"
	CmdWGPeerAdd    = "wg.peer.add"
	CmdWGPeerDelete = "wg.peer.delete"

	CmdUninstall = "panel.uninstall"
)

// Kode error terstruktur supaya layer API bisa memetakan ke HTTP status.
// Kode error spesifik. Ini kontrak: frontend menyusun kalimatnya sendiri
// dalam bahasa yang dipilih user, sedangkan `Error` tetap berisi kalimat
// bahasa Indonesia sebagai cadangan untuk klien non-browser (curl, skrip).
const (
	ErrPathTidakValid    = "path_invalid"
	ErrFolderTidakAda    = "folder_missing"
	ErrDiLuarHome        = "outside_home"
	ErrSymlinkKeluar     = "symlink_escape"
	ErrKomponenTidakAda  = "component_unknown"
	ErrBelumTerpasang    = "not_installed"
	ErrSudahAda          = "already_exists"
	ErrDikelolaLuar      = "managed_externally"
	ErrKredensialTidakOK = "credential_unreadable"
	ErrMasihTersambung   = "still_connected"
	ErrFuseTidakAda      = "fuse_missing"
	ErrNilaiTidakValid   = "value_invalid"
	ErrPasswordPendek    = "password_too_short"
	ErrGuestOKKonflik    = "guest_ok_conflict"
	ErrAksiBerjalan      = "action_in_progress"
	ErrDiskAdaFS         = "disk_has_filesystem"
	ErrDiskDipakai       = "disk_in_use"
	// ErrMirrorGagal: apt bisa membaca metadata repo tapi gagal mengunduh
	// berkas paketnya. Ini kegagalan mirror, bukan paket yang tidak ada, dan
	// perbedaan itu menentukan tindakan user: mengganti mirror, bukan mencari
	// nama paket lain.
	ErrMirrorGagal = "apt_mirror_failed"
)

const (
	ErrRequiresSudo = "requires_sudo"
	ErrNotFound     = "not_found"
	ErrDenied       = "denied"
	ErrInvalid      = "invalid"
	ErrInternal     = "internal"
)

type Request struct {
	Cmd string `json:"cmd"`
	// Username adalah identitas Linux user yang login, diambil dari session
	// server-side — tidak pernah dari input client.
	Username string          `json:"username"`
	Args     json.RawMessage `json:"args,omitempty"`
	TS       int64           `json:"ts"`
	Nonce    string          `json:"nonce"`
}

type Response struct {
	OK    bool            `json:"ok"`
	Code  string          `json:"code,omitempty"`
	Error string          `json:"error,omitempty"`
	Data  json.RawMessage `json:"data,omitempty"`
	// Params mengisi placeholder {0}, {1}, … pada kalimat yang disusun
	// frontend. Nilainya data (nama berkas, path, angka), bukan kalimat.
	Params []string `json:"params,omitempty"`
}

// ---- args per command (yang butuh struktur) ----

type LoginArgs struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type LoginResult struct {
	UID    int      `json:"uid"`
	GID    int      `json:"gid"`
	Home   string   `json:"home"`
	Shell  string   `json:"shell"`
	Sudo   bool     `json:"sudo"`
	Groups []string `json:"groups"`
}

type PasswdArgs struct {
	// Target kosong = ganti password sendiri (butuh OldPassword).
	// Target terisi = reset password user lain (butuh sudo).
	Target      string `json:"target,omitempty"`
	OldPassword string `json:"old_password,omitempty"`
	NewPassword string `json:"new_password"`
}

type PathArgs struct {
	Path string `json:"path"`
}

type TwoPathArgs struct {
	Source string `json:"source"`
	Dest   string `json:"dest"`
}

type FileEntry struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	IsDir   bool   `json:"is_dir"`
	Size    int64  `json:"size"`
	Mode    string `json:"mode"`
	ModePct uint32 `json:"mode_octal"`
	Owner   string `json:"owner"`
	Group   string `json:"group"`
	ModTime int64  `json:"mod_time"`
	Symlink string `json:"symlink,omitempty"`
}

// UsageHasil adalah ringkasan penelusuran isi satu direktori — setara `du -x`.
type UsageHasil struct {
	Size  int64 `json:"size"`
	Files int   `json:"files"`
	Dirs  int   `json:"dirs"`
	// Partial menandai penelusuran berhenti di batas, bukan karena habis.
	Partial bool `json:"partial"`
}

type WriteArgs struct {
	Path string `json:"path"`
	// Append dipakai untuk resume upload; default overwrite.
	Append bool `json:"append,omitempty"`
}

// ReadArgs membaca sebagian isi berkas. Offset/Length nol berarti seluruh
// berkas dari awal — bentuk yang dipakai unduhan dan preview teks.
//
// Rentang dibutuhkan pemutar media: <video> meminta potongan lewat header
// HTTP Range, dan tanpa jawaban 206 browser tidak bisa mencari posisi. MP4
// yang `moov` atom-nya berada di akhir berkas bahkan gagal diputar sama
// sekali, karena pemutar harus melompat ke ekor sebelum frame pertama.
type ReadArgs struct {
	Path   string `json:"path"`
	Offset int64  `json:"offset,omitempty"`
	// Length 0 = sampai akhir berkas.
	Length int64 `json:"length,omitempty"`
}

type ChmodArgs struct {
	Path string `json:"path"`
	Mode uint32 `json:"mode"` // oktal, mis. 0o644
}

type ChownArgs struct {
	Path      string `json:"path"`
	Owner     string `json:"owner"`
	Group     string `json:"group"`
	Recursive bool   `json:"recursive,omitempty"`
}

type KillArgs struct {
	PID    int `json:"pid"`
	Signal int `json:"signal"`
}

type ServiceArgs struct {
	Name   string `json:"name"`
	Action string `json:"action"` // start|stop|restart|enable|disable
}

type UfwRule struct {
	Num    string `json:"num,omitempty"`
	Action string `json:"action"` // allow|deny
	Port   string `json:"port"`
	Proto  string `json:"proto"` // tcp|udp|any
	From   string `json:"from,omitempty"`
	Raw    string `json:"raw,omitempty"`
}

// UfwUpdateArgs mengganti satu rule: rule lama dihapus, rule baru ditambahkan.
// ufw tidak punya perintah "edit" — nomor rule hanyalah posisi dalam daftar.
type UfwUpdateArgs struct {
	Num  string  `json:"num"`
	Spec string  `json:"spec,omitempty"`
	Rule UfwRule `json:"rule"`
}

type UfwDeleteArgs struct {
	Num string `json:"num"`
	// Spec dipakai saat ufw nonaktif: `ufw status numbered` tidak mengeluarkan
	// nomor apa pun di kondisi itu, dan indeks `ufw delete N` ikut menghitung
	// rule IPv6 sehingga bisa menghapus rule yang salah. Spec = bentuk rule
	// apa adanya, mis. "allow 22/tcp".
	Spec string `json:"spec,omitempty"`
}

// UfwToggleArgs: enable=true untuk nyalakan ufw, false untuk matikan.
// Frontend pakai field ini dari toggle switch di Settings → Firewall.
type UfwToggleArgs struct {
	Enable bool `json:"enable"`
}

type SambaShare struct {
	Name     string `json:"name"`
	Path     string `json:"path"`
	Writable bool   `json:"writable"`
	Public   bool   `json:"public"`
	Comment  string `json:"comment,omitempty"`
	// Users terdaftar di smbpasswd; password hanya dikirim saat set.
	ValidUsers []string `json:"valid_users,omitempty"`
	SmbUser    string   `json:"smb_user,omitempty"`
	SmbPass    string   `json:"smb_pass,omitempty"`
	// External menandai share yang sudah ada di smb.conf tapi bukan tulisan
	// panel. Ditampilkan apa adanya, tidak boleh diedit/dihapus dari panel —
	// menulisnya ke file include akan membuat definisi ganda di smbd.
	External bool `json:"external,omitempty"`
}

// Fail2banJail menggabungkan konfigurasi jail (jail.local) dengan status
// runtime-nya (fail2ban-client) — dua hal yang sering tidak sama.
type Fail2banJail struct {
	Name     string `json:"name"`
	Enabled  bool   `json:"enabled"`
	MaxRetry int    `json:"maxretry"`
	BanTime  string `json:"bantime,omitempty"`
	FindTime string `json:"findtime,omitempty"`
	Port     string `json:"port,omitempty"`
	// Running = jail benar-benar dimuat fail2ban, bukan sekadar enabled di file.
	Running         bool     `json:"running"`
	CurrentlyBanned int      `json:"currently_banned"`
	TotalBanned     int      `json:"total_banned"`
	CurrentlyFailed int      `json:"currently_failed"`
	TotalFailed     int      `json:"total_failed"`
	BannedIPs       []string `json:"banned_ips,omitempty"`
	External        bool     `json:"external,omitempty"`
}

type Fail2banUnbanArgs struct {
	Jail string `json:"jail"`
	IP   string `json:"ip"`
}

// Printer adalah satu antrean cetak CUPS.
type Printer struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Location    string `json:"location,omitempty"`
	URI         string `json:"uri,omitempty"`
	Model       string `json:"model,omitempty"`
	// State: idle | processing | stopped — apa adanya dari lpstat.
	State        string `json:"state"`
	StateMessage string `json:"state_message,omitempty"`
	Default      bool   `json:"default"`
	// Enabled = antrean menerima cetakan (cupsenable). Berbeda dari State:
	// antrean bisa "idle" tapi tetap ditolak kalau di-disable.
	Enabled bool `json:"enabled"`
	// Shared = printer dipublikasikan ke jaringan lewat CUPS/IPP.
	Shared bool `json:"shared"`
}

// PrinterAddArgs mendaftarkan antrean baru. URI berasal dari CmdPrinterDevices
// dan Model dari CmdPrinterModels — keduanya daftar tertutup dari CUPS sendiri,
// bukan teks bebas.
type PrinterAddArgs struct {
	Name        string `json:"name"`
	URI         string `json:"uri"`
	Model       string `json:"model"`
	Description string `json:"description,omitempty"`
	Location    string `json:"location,omitempty"`
	Shared      bool   `json:"shared,omitempty"`
}

// PrinterNameArgs dipakai aksi yang hanya butuh nama antrean.
type PrinterNameArgs struct {
	Name string `json:"name"`
	// Enable dipakai CmdPrinterEnable: true = cupsenable, false = cupsdisable.
	Enable bool `json:"enable,omitempty"`
}

// PrinterDevice adalah satu baris `lpinfo -v`: perangkat yang terdeteksi CUPS.
type PrinterDevice struct {
	URI  string `json:"uri"`
	Info string `json:"info,omitempty"`
}

// PrinterModel adalah satu baris `lpinfo -m`: driver/PPD yang tersedia.
type PrinterModel struct {
	Model string `json:"model"`
	Name  string `json:"name"`
}

// PrinterDeteksi adalah satu printer yang terpasang, digabung dengan status
// drivernya. Ini yang membuat halaman print server bisa menuntun user sampai
// selesai: perangkat yang terlihat tapi belum punya driver tidak berguna, dan
// tanpa informasi ini user hanya melihat antrean yang mencetak halaman kosong.
type PrinterDeteksi struct {
	URI  string `json:"uri"`
	Info string `json:"info,omitempty"`
	// Vendor dan Produk dipetik dari URI USB atau dari info lpinfo, dipakai
	// untuk mencari driver yang cocok sekaligus menentukan paket yang kurang.
	Vendor string `json:"vendor,omitempty"`
	Produk string `json:"produk,omitempty"`
	// Model = PPD yang cocok kalau sudah ada di sistem, siap dipakai lpadmin.
	Model     string `json:"model,omitempty"`
	ModelName string `json:"model_name,omitempty"`
	// SiapPakai = driver yang cocok sudah terpasang.
	SiapPakai bool `json:"siap_pakai"`
	// PaketDriver = paket yang perlu dipasang kalau SiapPakai false. Kosong
	// berarti panel tidak tahu paket mana yang cocok untuk vendor ini.
	PaketDriver []string `json:"paket_driver,omitempty"`
	// SudahTerdaftar = URI ini sudah punya antrean, jadi UI tidak menawarkan
	// menambahkannya dua kali.
	SudahTerdaftar bool `json:"sudah_terdaftar"`
}

// DriverInstallArgs memasang paket driver untuk satu vendor. Yang dikirim
// frontend adalah VENDOR, bukan nama paket: daftar paketnya ditentukan di
// backend supaya endpoint ini tidak pernah bisa dipakai memasang paket apa pun.
type DriverInstallArgs struct {
	Vendor string `json:"vendor"`
}

// PrintJob adalah satu pekerjaan di antrean.
type PrintJob struct {
	ID      string `json:"id"`
	Printer string `json:"printer"`
	User    string `json:"user"`
	Title   string `json:"title,omitempty"`
	Size    int64  `json:"size"`
	Waktu   int64  `json:"waktu,omitempty"`
}

// PrintFileArgs mencetak satu berkas milik user dari file manager.
type PrintFileArgs struct {
	Path    string `json:"path"`
	Printer string `json:"printer,omitempty"` // kosong = printer default
	Copies  int    `json:"copies,omitempty"`  // kosong/0 = 1
	// Media (A4, Letter) dan Sides (one-sided, two-sided-long-edge) diteruskan
	// ke lp sebagai -o. Kosong = pakai bawaan printer.
	Media string `json:"media,omitempty"`
	Sides string `json:"sides,omitempty"`
}

// PrintFileHasil mengembalikan id job supaya UI bisa memantau antreannya.
type PrintFileHasil struct {
	JobID   string `json:"job_id"`
	Printer string `json:"printer"`
}

// NFSExport adalah satu baris /etc/exports: satu folder dengan daftar klien.
type NFSExport struct {
	Path    string      `json:"path"`
	Clients []NFSClient `json:"clients"`
	// Active = benar-benar aktif di kernel (`exportfs -s`), bukan sekadar
	// tertulis di file.
	Active   bool `json:"active"`
	External bool `json:"external,omitempty"`
}

type NFSClient struct {
	Host    string `json:"host"`
	Options string `json:"options,omitempty"`
}

// NFSMount adalah satu export milik server LAIN yang dipasang di mesin ini —
// sisi klien dari halaman NFS. Barisnya ditulis ke /etc/fstab supaya mount
// bertahan setelah reboot, dengan pola penanda yang sama seperti pool mergerfs.
type NFSMount struct {
	Server     string `json:"server"`     // nas.home atau 192.168.2.11
	Remote     string `json:"remote"`     // path export DI SERVER itu
	Mountpoint string `json:"mountpoint"` // folder di mesin ini
	Options    string `json:"options,omitempty"`
	Mounted    bool   `json:"mounted"`
	// InFstab=false berarti mount hidup yang tidak tercatat di /etc/fstab —
	// dipasang manual lewat `mount`, dan akan hilang setelah reboot.
	InFstab bool `json:"in_fstab"`
	// External = tidak ditulis panel (baris fstab orang lain, atau mount
	// manual). Ditampilkan apa adanya, tidak pernah diubah dari sini.
	External bool   `json:"external,omitempty"`
	Total    uint64 `json:"total,omitempty"`
	Used     uint64 `json:"used,omitempty"`
	Free     uint64 `json:"free,omitempty"`
}

// NFSMountToggleArgs memasang atau melepas mount yang barisnya sudah ada di
// fstab, tanpa menyentuh barisnya — mount yang dilepas kembali terpasang saat
// boot berikutnya.
type NFSMountToggleArgs struct {
	Mountpoint string `json:"mountpoint"`
	Lepas      bool   `json:"lepas"`
}

// NFSDiscoverArgs menanyakan daftar export yang ditawarkan satu server
// (showmount -e), supaya path remote tidak perlu diketik dari ingatan.
type NFSDiscoverArgs struct {
	Server string `json:"server"`
}

// NFSRemoteExport adalah satu baris balasan showmount -e.
type NFSRemoteExport struct {
	Path    string `json:"path"`
	Clients string `json:"clients,omitempty"`
}

// DiskPrepareArgs menyiapkan satu disk mentah supaya bisa dipakai: format
// (kalau diminta), daftarkan di /etc/fstab lewat UUID, lalu mount.
type DiskPrepareArgs struct {
	Path       string `json:"path"`       // /dev/sdb — harus disk utuh, bukan partisi
	Mountpoint string `json:"mountpoint"` // /mnt/data
	FSType     string `json:"fstype"`     // ext4 | xfs | btrfs; diabaikan kalau Format=false
	// Format=false berarti disknya sudah punya filesystem dan hanya perlu
	// di-mount. Ini default yang aman: memformat menghapus isi disk.
	Format bool `json:"format"`
	// Timpa adalah izin eksplisit untuk memformat disk yang SUDAH berisi
	// filesystem. Tanpa ini helper menolak, supaya satu klik salah tidak
	// menghapus data yang sudah ada di sana.
	Timpa bool `json:"timpa"`
}

// MergerfsPool adalah satu baris fuse.mergerfs di /etc/fstab.
type MergerfsPool struct {
	Mountpoint string   `json:"mountpoint"`
	Branches   []string `json:"branches"`
	Options    string   `json:"options,omitempty"`
	Mounted    bool     `json:"mounted"`
	// External = baris fstab yang bukan tulisan panel; ditampilkan apa adanya
	// dan tidak boleh diubah dari sini.
	External bool `json:"external,omitempty"`
	// Kapasitas hanya terisi kalau pool sedang ter-mount.
	Total uint64 `json:"total,omitempty"`
	Used  uint64 `json:"used,omitempty"`
	Free  uint64 `json:"free,omitempty"`
}

// MergerfsMountArgs memasang atau melepas pool yang barisnya sudah ada di
// fstab. Barisnya sendiri tidak disentuh, jadi pool yang dilepas akan
// terpasang lagi saat boot berikutnya — untuk membuangnya permanen, hapus
// pool-nya.
type MergerfsMountArgs struct {
	Mountpoint string `json:"mountpoint"`
	Lepas      bool   `json:"lepas"`
}

// SambaUser adalah entri di database smbpasswd. Password tidak pernah dibaca
// balik dari sistem — field Password hanya dipakai saat menyimpan.
type SambaUser struct {
	Username string `json:"username"`
	Enabled  bool   `json:"enabled"`
	Password string `json:"password,omitempty"`
}

type SambaUserArgs struct {
	Username string `json:"username"`
	Password string `json:"password,omitempty"`
	// Disable mengunci akun Samba tanpa menghapusnya (smbpasswd -d).
	Disable bool `json:"disable,omitempty"`
}

type LinuxUser struct {
	Username string   `json:"username"`
	UID      int      `json:"uid"`
	GID      int      `json:"gid"`
	Home     string   `json:"home"`
	Shell    string   `json:"shell"`
	Groups   []string `json:"groups"`
	Locked   bool     `json:"locked"`
	Comment  string   `json:"comment"`
}

type UserCreateArgs struct {
	Username string   `json:"username"`
	Password string   `json:"password"`
	Shell    string   `json:"shell,omitempty"`
	Groups   []string `json:"groups,omitempty"`
	Comment  string   `json:"comment,omitempty"`
	MakeHome bool     `json:"make_home"`
}

type UserModifyArgs struct {
	Username string   `json:"username"`
	Shell    string   `json:"shell,omitempty"`
	Groups   []string `json:"groups,omitempty"`
	Comment  string   `json:"comment,omitempty"`
	Lock     *bool    `json:"lock,omitempty"`
}

type UserDeleteArgs struct {
	Username   string `json:"username"`
	RemoveHome bool   `json:"remove_home"`
}

type ComponentArgs struct {
	Name   string `json:"name"`
	Action string `json:"action,omitempty"` // untuk component.service
	// Fresh meminta helper membuang cache status sebelum memeriksa ulang.
	// Dipakai tombol Refresh manual — pemuatan biasa tetap boleh memakai
	// cache supaya membuka halaman tidak memicu ~30 probe proses.
	Fresh bool `json:"fresh,omitempty"`
	// Purge meminta uninstall ikut menghapus data milik komponen, bukan cuma
	// paketnya. Harus diminta eksplisit: yang dihapus tidak bisa dikembalikan
	// dan sebagiannya (API key, koneksi provider) tidak dibuat oleh panel.
	Purge bool `json:"purge,omitempty"`
}

// ComponentProgress adalah kemajuan instalasi yang sedang berjalan.
//
// Persen berasal dari apt sendiri (APT::Status-Fd), bukan dari perkiraan waktu:
// angka yang ditebak dari stopwatch akan berbohong pada mesin lambat dan pada
// paket besar, justru dua keadaan yang paling butuh keterangan jujur.
type ComponentProgress struct {
	Name string `json:"name"`
	// Jenis: "install" | "uninstall". Halaman yang baru dimuat di tengah
	// pekerjaan hanya punya laporan ini untuk tahu aksi apa yang berjalan —
	// tanpa itu ia harus menebak, dan tebakan yang salah membuat kartu
	// mengaku sedang memasang komponen yang justru sedang dicopot.
	Jenis string `json:"jenis,omitempty"`
	// Persen 0..100 untuk keseluruhan proses, sudah menggabungkan tahap
	// pembaruan indeks, pengunduhan, dan pemasangan.
	Persen int `json:"persen"`
	// Fase: "indeks" | "unduh" | "pasang" — dipakai UI sebagai keterangan
	// singkat di bawah bar.
	Fase  string `json:"fase,omitempty"`
	Pesan string `json:"pesan,omitempty"`
	// Aktif false berarti tidak ada instalasi yang sedang berjalan.
	Aktif bool `json:"aktif"`
}

type ComponentStatus struct {
	Name      string `json:"name"`
	Installed bool   `json:"installed"`
	Version   string `json:"version,omitempty"`
	Running   bool   `json:"running"`
	Service   string `json:"service,omitempty"`
	// Category & Description dipakai halaman Components untuk mengelompokkan
	// dan menjelaskan software opsional yang tidak ikut di instalasi dasar
	// Ubuntu/Debian.
	Category    string `json:"category,omitempty"`
	Description string `json:"description,omitempty"`
	// RequiredFor diisi kalau ada halaman panel yang tidak bisa dipakai tanpa
	// komponen ini — halaman tersebut menampilkan "Belum Terpasang" alih-alih
	// daftar kosong atau error mentah dari command yang tidak ada.
	RequiredFor string `json:"required_for,omitempty"`
	// KelolaDi diisi kalau service komponen ini dikendalikan halaman lain —
	// cloudflared dijalankan dari Settings → Network bersama token tunnelnya.
	// Halaman Components menampilkan keterangan itu, bukan tombol jalankan.
	KelolaDi string `json:"managed_in,omitempty"`
	// Note membawa keterangan yang hanya relevan setelah komponen terpasang
	// dan tidak bisa ditulis di Description statis — mis. kredensial awal
	// yang dibuat panel untuk 9router.
	Note string `json:"note,omitempty"`
	// PunyaData menandai komponen yang menyimpan data di luar paketnya, jadi
	// halaman Components bisa menawarkan "hapus data juga" saat uninstall —
	// hanya untuk komponen yang memang punya sesuatu untuk dihapus.
	PunyaData bool `json:"has_data,omitempty"`
}

type DockerExecArgs struct {
	// Args adalah argumen array untuk binary `docker` — TIDAK PERNAH string
	// shell. Helper memvalidasi subcommand terhadap whitelist.
	Args []string `json:"args"`
	Dir  string   `json:"dir,omitempty"`
}

type ExecResult struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exit_code"`
}

// VPNArgs dipakai untuk kelompok VPN/Tunnel di Settings → Network:
// Tailscale, Cloudflare Tunnel, dan WireGuard.
type VPNArgs struct {
	Name    string `json:"name"`   // tailscale|cloudflared|wireguard
	Action  string `json:"action"` // up|down (khusus wireguard: juga "remove")
	AuthKey string `json:"auth_key,omitempty"`
	Token   string `json:"token,omitempty"`
	Config  string `json:"config,omitempty"` // isi wg0.conf
	Host    string `json:"hostname,omitempty"`
}

type VPNStatus struct {
	Name      string `json:"name"`
	Installed bool   `json:"installed"`
	Connected bool   `json:"connected"`
	State     string `json:"state"`
	Detail    string `json:"detail,omitempty"`
	// Token adalah token tunnel Cloudflare yang sudah terpasang di unit
	// systemd. Dikirim balik supaya halaman Network bisa menampilkan tunnel
	// mana yang aktif, bukan kotak kosong yang menyesatkan.
	Token string `json:"token,omitempty"`
	// NeedsApproval: node sudah terdaftar di tailnet tapi admin belum
	// menyetujuinya (fitur Device approval Tailscale). Bukan kegagalan —
	// tidak ada yang bisa diperbaiki di mesin ini — jadi dibedakan dari
	// Connected=false biasa supaya panel tidak menampilkannya sebagai error.
	NeedsApproval bool `json:"needs_approval,omitempty"`
}

type TerminalArgs struct {
	Cols    uint16 `json:"cols"`
	Rows    uint16 `json:"rows"`
	Command string `json:"command,omitempty"`
}

type DNSArgs struct {
	Nameservers []string `json:"nameservers"`
}

// Frame type untuk stream terminal (arah client → helper).
// Format: [1 byte type][4 byte big-endian length][payload]
const (
	TermFrameData   byte = 0
	TermFrameResize byte = 1
)

// Sign menghitung HMAC-SHA256 dari payload request.
func Sign(secret, payload []byte) string {
	m := hmac.New(sha256.New, secret)
	m.Write(payload)
	return hex.EncodeToString(m.Sum(nil))
}

// Verify membandingkan signature secara constant-time.
func Verify(secret, payload []byte, sig string) bool {
	want, err := hex.DecodeString(sig)
	if err != nil {
		return false
	}
	m := hmac.New(sha256.New, secret)
	m.Write(payload)
	return hmac.Equal(m.Sum(nil), want)
}

// UpdateStatus melaporkan pembaruan panel dari repo: versi yang terpasang,
// versi di remote, dan jalannya proses build+install yang sedang berlangsung.
type UpdateStatus struct {
	Running bool `json:"running"`
	// Log adalah ekor keluaran skrip pembaruan apa adanya — dipakai UI sebagai
	// tampilan shell, jadi tidak diterjemahkan.
	Log string `json:"log"`
	// Result & Exit diisi setelah proses selesai: "success" atau alasan gagal
	// dari systemd, plus exit code skripnya.
	Result string `json:"result,omitempty"`
	Exit   int    `json:"exit"`
	// Lokal & Remote berisi commit pendek (mis. "3a45c7e ubah ini"), Remote
	// hanya diisi kalau pengecekan ke remote diminta.
	Lokal      string `json:"lokal,omitempty"`
	Remote     string `json:"remote,omitempty"`
	Tertinggal bool   `json:"tertinggal"`
	// Perubahan berisi judul commit yang ada di remote tapi belum terpasang,
	// terbaru dulu — isi modal Update supaya user tahu apa yang akan dipasang
	// sebelum menekan tombolnya. Hanya diisi kalau diminta (UpdateArgs.Rinci):
	// mengambilnya butuh fetch, sementara pengecekan versi biasa jalan tiap
	// lima menit di latar.
	Perubahan []string `json:"perubahan,omitempty"`
	// PerubahanPasti = commit yang terpasang ketemu di riwayat yang diambil,
	// jadi daftar di atas benar-benar "yang belum terpasang". Kalau false,
	// daftarnya adalah commit terbaru di remote apa adanya — terjadi saat
	// riwayat lokal tidak menyambung ke remote (checkout dangkal dari sumber
	// lain, atau ketinggalan lebih jauh dari jendela yang diambil).
	PerubahanPasti bool `json:"perubahan_pasti,omitempty"`
}

// UpdateArgs menyalakan pengecekan versi remote — sengaja opsional karena
// pengecekan itu memerlukan jaringan dan tidak boleh ikut tiap polling log.
type UpdateArgs struct {
	Cek bool `json:"cek"`
	// Rinci ikut mengambil daftar commit yang belum terpasang. Dipisah dari
	// Cek karena butuh `git fetch` — pengecekan versi di sidebar yang jalan
	// tiap lima menit cukup dengan `git ls-remote` yang tidak menarik objek.
	Rinci bool `json:"rinci,omitempty"`
}

// WireGuard mode server: panel yang membuat config, kunci, NAT, dan daftar
// peer-nya. Mode klien tetap memakai VPNArgs.Config (tempel config apa adanya).

type WGServerArgs struct {
	// Subnet tunnel dalam CIDR, mis. "10.8.0.0/24". Alamat pertama dipakai
	// server sendiri.
	Subnet string `json:"subnet"`
	Port   int    `json:"port"`
	// Endpoint adalah alamat yang dituju klien dari luar — IP publik atau
	// hostname. Tidak bisa disimpulkan sendiri oleh server di balik NAT.
	Endpoint string `json:"endpoint"`
}

type WGPeerArgs struct {
	Nama      string `json:"nama"`
	PublicKey string `json:"public_key,omitempty"`
}

type WGPeer struct {
	Nama      string `json:"nama"`
	PublicKey string `json:"public_key"`
	IP        string `json:"ip"`
	// Handshake & Transfer diisi dari `wg show` kalau interface sedang hidup.
	Handshake string `json:"handshake,omitempty"`
	Transfer  string `json:"transfer,omitempty"`
}

// WGServerInfo menggambarkan config WireGuard yang ada di sistem, termasuk
// yang dibuat di luar panel: Server bernilai true begitu config punya
// ListenPort, apa pun yang membuatnya.
type WGServerInfo struct {
	Ada      bool     `json:"ada"`
	Server   bool     `json:"server"`
	Iface    string   `json:"iface"`
	Subnet   string   `json:"subnet,omitempty"`
	Port     int      `json:"port,omitempty"`
	Endpoint string   `json:"endpoint,omitempty"`
	Peers    []WGPeer `json:"peers"`
}

// WGPeerBaru memuat config klien lengkap. Private key klien TIDAK disimpan di
// server, jadi isinya hanya bisa dilihat sekali — sesudah itu klien harus
// dibuat ulang kalau confignya hilang.
type WGPeerBaru struct {
	Peer   WGPeer `json:"peer"`
	Config string `json:"config"`
}

// UninstallArgs menjalankan uninstall panel. Mode bertingkat:
//
//	panel      → binary, unit systemd, PAM, dan sumber di /usr/local/src
//	panel-data → + data & config panel (/var/lib/linux-dashboard) + akun service
//	total      → + copot components yang dipasang lewat halaman Components
//
// Berkas pribadi user di ~/DATA/* tidak pernah ikut dihapus mode mana pun.
type UninstallArgs struct {
	Mode string `json:"mode"`
	// Password akun yang sedang login (selalu sudoer), diverifikasi lewat PAM
	// tepat sebelum uninstall dimulai. Tidak pernah ikut dicatat ke log.
	Password string `json:"password"`
}
