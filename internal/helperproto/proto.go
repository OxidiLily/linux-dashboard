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
	// CmdAuthLogout mencabut token yang dipakai mengirim permintaan ini. Dipakai
	// web app saat logout, saat sesi dicabut, dan saat password diganti, supaya
	// token sesi lama tidak hidup lebih lama dari sesi panelnya.
	CmdAuthLogout = "auth.logout"
	// CmdAuthSudo melaporkan status sudo TERKINI pemilik token: helper
	// memeriksa keanggotaan grup akun itu saat permintaan diterima, bukan
	// menyalin nilai yang tersimpan sejak login.
	//
	// Web app menyimpan status sudo di baris sesinya saat login, dan
	// keanggotaan grup sudo bisa dicabut di luar panel (`deluser x sudo`).
	// Tanpa command ini, salinan itu tetap dipakai sampai sesinya berakhir:
	// endpoint yang butuh sudo masih terbuka di sisi API, dan user melihat
	// kegagalan dari tempat yang jauh dari sebabnya.
	CmdAuthSudo           = "auth.sudo"
	CmdAuthPasswordStatus = "auth.password_status"

	CmdSysHostnameSet = "sys.hostname_set"
	CmdSysDNSSet      = "sys.dns_set"
	CmdNetIfaceGet    = "net.iface_get"
	CmdNetIfaceSet    = "net.iface_set"

	CmdProcKill = "proc.kill"

	CmdSvcAction = "svc.action"

	CmdUfwStatus = "ufw.status"
	CmdUfwAdd    = "ufw.add"
	CmdUfwDelete = "ufw.delete"
	CmdUfwUpdate = "ufw.update"
	CmdUfwToggle = "ufw.toggle"

	CmdFileList   = "file.list"
	CmdFileSearch = "file.search"
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

	// Cronjob: crontab MILIK AKUN YANG LOGIN, dibaca dan ditulis sebagai
	// identitas akun itu — bukan crontab root, bukan /etc/crontab. Satu akun
	// panel tidak pernah bisa melihat atau menyentuh jadwal akun lain.
	CmdCronGet = "cron.get"
	CmdCronPut = "cron.put"

	// Disk mentah: format (opsional) lalu daftarkan di fstab dan mount.
	CmdDiskPrepare = "disk.prepare"
	// Kebalikannya: lepas mount disk, dan (kalau diminta) buang jejaknya dari
	// fstab beserta folder mount point-nya.
	CmdDiskUnmount = "disk.unmount"

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
	CmdComponentService   = "component.service" // start/stop/restart, update (9router)
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

	CmdUninstall = "panel.uninstall"
	CmdReboot    = "system.reboot"
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
	// ErrCronConflict: crontab berubah di antara saat UI memuatnya dan saat
	// menekan Simpan. Tanpa penjaga ini, tab yang sudah lama terbuka akan
	// menimpa jadwal yang barusan ditulis dari tempat lain — dan cron tidak
	// punya riwayat. UI menjawabnya dengan memuat ulang, bukan menimpa.
	ErrCronConflict = "cron_conflict"
)

const (
	ErrRequiresSudo = "requires_sudo"
	ErrNotFound     = "not_found"
	ErrDenied       = "denied"
	ErrInvalid      = "invalid"
	ErrInternal     = "internal"
	// ErrSesiTidakValid: token capability yang dikirim tidak ada, sudah
	// dicabut, atau sudah kedaluwarsa. Dipisahkan dari ErrDenied supaya layer
	// API bisa memetakannya ke HTTP 401 (sesi tidak sah) alih-alih 403 —
	// "sesi Anda sudah berakhir" dan "aksi ini butuh sudo" adalah dua hal
	// berbeda bagi user, dan yang pertama harus memaksanya login ulang.
	ErrSesiTidakValid = "session_invalid"
)

type Request struct {
	Cmd string `json:"cmd"`
	// Username TIDAK LAGI dipakai untuk memutuskan hak apa pun. Ia hanya
	// keterangan untuk log. Otorisasi diambil dari Token.
	Username string `json:"username,omitempty"`
	// Token adalah capability opaque yang diterbitkan helper saat login PAM
	// berhasil (lihat LoginResult.Token). Helper menentukan identitas — dan
	// karenanya seluruh hak — dari token ini, bukan dari klaim pemanggil.
	// Permintaan tanpa token yang sah selalu ditolak.
	Token string          `json:"token,omitempty"`
	Args  json.RawMessage `json:"args,omitempty"`
	TS    int64           `json:"ts"`
	Nonce string          `json:"nonce"`
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
	// TTLHours adalah umur sesi panel yang diminta web app, dalam jam
	// (config.SessionTTLHours, env DASHBOARD_SESSION_TTL_HOURS). Token yang
	// diterbitkan helper harus hidup selama sesinya — token yang mati lebih
	// dulu membuat setiap permintaan berikutnya dijawab 401 dan user dipaksa
	// login ulang tanpa sebab yang terlihat.
	//
	// Nilai <= 0 berarti pemanggil tidak menentukan apa pun: helper memakai
	// umur bawaannya. Nilai yang keterlaluan dijepit helper (lihat ttlToken),
	// jadi klien tidak bisa meminta token abadi.
	TTLHours int `json:"ttl_hours,omitempty"`
}

type LoginResult struct {
	UID    int      `json:"uid"`
	GID    int      `json:"gid"`
	Home   string   `json:"home"`
	Shell  string   `json:"shell"`
	Sudo   bool     `json:"sudo"`
	Groups []string `json:"groups"`
	// MustChangePassword dihitung helper root dari metadata expiry akun. Web app
	// tidak bisa menjalankan chage karena sengaja berjalan tanpa akses shadow.
	MustChangePassword bool `json:"must_change_password"`
	// Token adalah capability opaque yang harus dikirim balik pada setiap
	// permintaan berikutnya. Web app menyimpannya bersama sesinya; ia tidak
	// pernah dikirim ke browser.
	Token string `json:"token"`
}

// SudoResult adalah jawaban CmdAuthSudo: apakah pemilik token MASIH anggota
// grup sudo menurut keadaan akun saat itu juga.
//
// Hanya satu bit, dan itu disengaja: pertanyaan yang dijawab di sini adalah
// "hak ini masih ada?", bukan "siapa Anda?" — identitasnya sudah ditentukan
// token, dan mengirim ulang identitas hanya memberi alasan bagi pemanggil
// untuk memakainya sebagai dasar otorisasi lagi.
type SudoResult struct {
	Sudo bool `json:"sudo"`
}

type PasswordStatusResult struct {
	MustChangePassword bool `json:"must_change_password"`
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

// SearchArgs adalah permintaan pencarian nama berkas secara rekursif —
// setara `grep -r` pada nama, bukan isi. Isi berkas sengaja tidak dibaca:
// folder data user bisa berisi puluhan GB, dan membaca semuanya untuk satu
// kata kunci membuat pencarian mustahil dipakai.
type SearchArgs struct {
	// Path adalah folder awal; pencarian menelusuri seluruh subfoldernya.
	Path string `json:"path"`
	// Query adalah potongan nama yang dicari, kapital diabaikan.
	Query string `json:"query"`
	// Maks membatasi jumlah hasil. 0 = pakai batas bawaan helper.
	Maks int `json:"maks,omitempty"`
}

// SearchHit adalah satu berkas atau folder yang cocok.
type SearchHit struct {
	Name string `json:"name"`
	Path string `json:"path"`
	// Rel adalah lokasi relatif terhadap folder awal pencarian, supaya UI
	// bisa menuliskan "sub/dalam/berkas.txt" tanpa menghitung sendiri.
	Rel     string `json:"rel"`
	IsDir   bool   `json:"is_dir"`
	Size    int64  `json:"size"`
	ModTime int64  `json:"mod_time"`
}

// SearchHasil adalah jawaban pencarian beserta batas yang tercapai.
type SearchHasil struct {
	Hits []SearchHit `json:"hits"`
	// Truncated menandai hasil berhenti di batas, bukan karena pohonnya habis.
	// Tanpa ini, daftar yang terpotong terlihat seperti daftar lengkap.
	Truncated bool `json:"truncated"`
	// Alasan menyebut KENAPA berhenti: "hasil", "folder", atau "waktu".
	// Dipisah dari Truncated karena kalimat yang benar berbeda untuk tiap
	// sebab — "persempit kata kunci" tidak menolong saat yang habis waktunya.
	// Kosong = penelusuran selesai menyeluruh.
	Alasan string `json:"alasan,omitempty"`
	// Dirs adalah jumlah folder yang benar-benar dikunjungi — dipakai UI untuk
	// menjelaskan bahwa pencariannya memang masuk ke dalam subfolder.
	Dirs int `json:"dirs"`
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
	// HanyaMilikRoot membatasi perubahan pada berkas yang pemiliknya MASIH
	// root, dan mendiamkan sisanya tanpa error.
	//
	// Dipakai jalur perbaikan otomatis panel: berkas yang dibuat helper daemon
	// (root) boleh diserahkan ke admin yang menyuntingnya lewat panel, tapi
	// berkas milik admin LAIN tidak boleh diambil alih diam-diam hanya karena
	// seseorang menekan Simpan.
	HanyaMilikRoot bool `json:"hanya_milik_root,omitempty"`
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
	// Comment adalah label pemilik rule: `ufw status` menampilkannya di kolom
	// paling kanan ("445/tcp ALLOW IN Anywhere # Samba"), jadi dari daftar rule
	// saja sudah terbaca layanan mana yang membukanya. Sebagian rule lama tidak
	// punya label — reconciler port komponen yang menambahkan.
	Comment string `json:"comment,omitempty"`
	Raw     string `json:"raw,omitempty"`
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

// CronMaxBytes adalah batas isi crontab yang diterima panel, dihitung dari
// byte UTF-8 (bukan jumlah karakter).
//
// crontab sendiri tidak memasang plafon serendah ini — batasnya jauh lebih
// besar. Angka 64 KiB dipilih dari sisi lain: berkas spool crontab dibaca
// seluruhnya berkali-kali oleh setiap proses cron, dan tidak ada crontab
// sungguhan yang perlu lebih besar. Isi di bawah plafon ini ditolak dengan
// kalimat yang jelas, bukan dipotong diam-diam.
const CronMaxBytes = 64 << 10

// CronArgs mengubah crontab akun yang mengirim permintaan.
type CronArgs struct {
	// Isi adalah crontab lengkap yang akan dipasang. String kosong = hapus
	// semua jadwal (crontab kosong, bukan "tidak ada crontab").
	Isi string `json:"isi"`
	// Previous adalah isi crontab yang terakhir DILIHAT UI, apa adanya.
	//
	// Penunjuk, bukan string: "belum pernah memuat" harus bisa dibedakan dari
	// "memuat crontab yang memang kosong". Tanpa pembedaan itu, klien yang
	// melewatkan field ini akan lolos sebagai penimpa bebas — persis kejadian
	// yang penjaga ini ada untuk mencegahnya.
	Previous *string `json:"previous"`
}

// CronHasil adalah isi crontab setelah operasi selesai — dibaca ulang dari
// crontab, bukan dikutip dari yang dikirim klien. Penulisan yang "berhasil"
// tapi tidak mendarat akan terlihat di sini.
type CronHasil struct {
	Isi string `json:"isi"`
	// Batas ikut dikirim supaya UI memakai angka yang sama dengan server —
	// dua angka batas yang ditulis terpisah pasti akan berbeda suatu hari.
	Batas int `json:"batas"`
	// Layanan & LayananAktif hanya diisi cron.get: nama unit penjadwal yang
	// ditemukan di mesin (cron/crond) dan apakah ia benar-benar berjalan.
	// Crontab yang tidak pernah dijalankan siapa pun adalah kegagalan diam
	// yang paling sulit dilacak dari UI.
	Layanan      string `json:"layanan,omitempty"`
	LayananAktif bool   `json:"layanan_aktif,omitempty"`
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

// DiskUnmountArgs melepas satu mount disk. Lupakan=false hanya umount —
// barisnya tetap di fstab, jadi mount-nya kembali setelah boot. Lupakan=true
// sekalian membuang baris fstab tulisan panel dan folder mount point-nya.
type DiskUnmountArgs struct {
	Mountpoint string `json:"mountpoint"`
	Lupakan    bool   `json:"lupakan"`
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
	// VersiBaru diisi kalau registry paketnya sudah punya versi yang lebih
	// baru dari yang terpasang — halaman Components menampilkan tombol
	// Perbarui. Baru 9router yang mengisinya (lihat versiTerbaruNpm).
	VersiBaru string `json:"latest_version,omitempty"`
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
// Tailscale dan Cloudflare Tunnel.
type VPNArgs struct {
	Name    string `json:"name"`   // tailscale|cloudflared
	Action  string `json:"action"` // up|down
	AuthKey string `json:"auth_key,omitempty"`
	Token   string `json:"token,omitempty"`
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

// IfaceConfig adalah konfigurasi IP satu interface dari netplan, dalam
// bentuk yang sama dengan pengaturan jaringan Ubuntu: satu mode per keluarga
// alamat, bukan berkas YAML mentah.
type IfaceConfig struct {
	Iface string `json:"iface"`
	// Managed: interface dikenal netplan (ada di salah satu berkas
	// /etc/netplan). Interface virtual (docker0, veth) tidak, dan tidak bisa
	// diedit dari sini.
	Managed bool     `json:"managed"`
	IPv4    string   `json:"ipv4"` // dhcp | static | off
	Addrs4  []string `json:"addrs4"`
	Gw4     string   `json:"gw4,omitempty"`
	IPv6    string   `json:"ipv6"` // auto | static | off
	Addrs6  []string `json:"addrs6"`
	Gw6     string   `json:"gw6,omitempty"`
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
	// Jarak = berapa commit yang belum terpasang, yaitu panjang daftar di atas.
	// Isinya bermakna bersama PerubahanPasti: kalau PerubahanPasti=false,
	// angkanya adalah batas jendela (mis. 20) dan artinya "sekurang-kurangnya
	// sebanyak itu" — panel yang tertinggal puluhan commit memang tidak bisa
	// dihitung tepat dari checkout dangkal.
	//
	// Angka ini yang membuat sifat "sekali tekan = versi terakhir" terlihat di
	// modal: user tahu yang dipasang adalah ujung branch, berapa pun jumlah
	// commit yang dilompati.
	Jarak int `json:"jarak,omitempty"`
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

// UninstallArgs menjalankan uninstall panel. Mode bertingkat:
//
//	panel      → binary, unit systemd, PAM, dan sumber di /usr/local/src
//	panel-data → + data & config panel (/var/lib/linux-dashboard) + akun service
//	total      → + copot components yang dipasang lewat halaman Components
//	total-data → + hapus folder data akun (~/DATA) di setiap home dan /etc/skel
//
// Berkas pribadi user di ~/DATA/* tidak ikut dihapus mode mana pun kecuali
// total-data.
type UninstallArgs struct {
	Mode string `json:"mode"`
	// Password akun yang sedang login (selalu sudoer), diverifikasi lewat PAM
	// tepat sebelum uninstall dimulai. Tidak pernah ikut dicatat ke log.
	Password string `json:"password"`
}
