package helper

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"

	"linux-dashboard/OxidiLily/internal/helperproto"
)

type userInfo struct {
	Name   string
	UID    int
	GID    int
	Home   string
	Shell  string
	Sudo   bool
	Groups []uint32
	Names  []string
}

func (u *userInfo) credential() *syscall.Credential {
	return &syscall.Credential{
		Uid:    uint32(u.UID),
		Gid:    uint32(u.GID),
		Groups: u.Groups,
	}
}

// lookupUser mengumpulkan identitas Linux user + status sudo.
//
// root (UID 0) di-bypass duluan dan TIDAK dicek lewat keanggotaan grup:
// root memang tidak pernah jadi anggota grup `sudo` di Debian/Ubuntu, jadi
// otorisasi yang hanya mengandalkan cek grup akan salah menolak root.
func lookupUser(username string) (*userInfo, error) {
	u, err := user.Lookup(username)
	if err != nil {
		return nil, fmt.Errorf("user %q tidak ditemukan: %w", username, err)
	}
	uid, err := strconv.Atoi(u.Uid)
	if err != nil {
		return nil, err
	}
	gid, err := strconv.Atoi(u.Gid)
	if err != nil {
		return nil, err
	}
	info := &userInfo{Name: u.Username, UID: uid, GID: gid, Home: u.HomeDir}
	info.Shell = loginShell(u.Username)

	gids, err := u.GroupIds()
	if err != nil {
		return nil, err
	}
	for _, g := range gids {
		n, err := strconv.Atoi(g)
		if err != nil {
			continue
		}
		info.Groups = append(info.Groups, uint32(n))
		if grp, err := user.LookupGroupId(g); err == nil {
			info.Names = append(info.Names, grp.Name)
		}
	}

	if uid == 0 {
		info.Sudo = true
		return info, nil
	}
	if sudoGrp, err := user.LookupGroup("sudo"); err == nil {
		info.Sudo = slices.Contains(gids, sudoGrp.Gid)
	}
	// Debian murni kadang pakai grup `admin` sebagai padanan historis.
	if !info.Sudo {
		if adminGrp, err := user.LookupGroup("admin"); err == nil {
			info.Sudo = slices.Contains(gids, adminGrp.Gid)
		}
	}
	return info, nil
}

// sudoRequired menandai command yang hanya boleh dijalankan root/anggota sudo.
var sudoRequired = map[string]bool{
	helperproto.CmdSysHostnameSet: true,
	helperproto.CmdSysDNSSet:      true,
	helperproto.CmdSvcAction:      true,
	helperproto.CmdUfwStatus:      true,
	helperproto.CmdUfwAdd:         true,
	helperproto.CmdUfwDelete:      true,
	helperproto.CmdUfwUpdate:      true,
	helperproto.CmdUfwToggle:      true,
	helperproto.CmdSambaList:      true,
	helperproto.CmdSambaSave:      true,
	helperproto.CmdSambaDelete:    true,
	helperproto.CmdNFSList:        true,
	helperproto.CmdNFSSave:        true,
	helperproto.CmdNFSDelete:      true,
	helperproto.CmdFail2banList:   true,
	helperproto.CmdFail2banSave:   true,
	helperproto.CmdFail2banDelete: true,
	helperproto.CmdFail2banUnban:  true,
	// Format disk menghapus data dan menulis /etc/fstab — jelas sudo.
	helperproto.CmdDiskPrepare:        true,
	helperproto.CmdMergerfsList:       true,
	helperproto.CmdMergerfsSave:       true,
	helperproto.CmdMergerfsDelete:     true,
	helperproto.CmdMergerfsMount:      true,
	helperproto.CmdSambaUserList:      true,
	helperproto.CmdSambaUserSet:       true,
	helperproto.CmdSambaUserDelete:    true,
	helperproto.CmdUserList:           true,
	helperproto.CmdUserCreate:         true,
	helperproto.CmdUserModify:         true,
	helperproto.CmdUserDelete:         true,
	helperproto.CmdComponentInstall:   true,
	helperproto.CmdComponentUninstall: true,
	helperproto.CmdComponentService:   true,
	helperproto.CmdComponentProgress:  true,
	helperproto.CmdDockerExec:         true,
	helperproto.CmdFileChown:          true,
	helperproto.CmdVPNStatus:          true,
	helperproto.CmdVPNConfigure:       true,
	// WireGuard mode server menulis /etc/wireguard dan menjalankan wg-quick
	// sebagai root; pembaruan panel menjalankan install.sh sebagai root.
	// Web app memang sudah menggate keduanya dengan requireSudo, tapi helper
	// adalah penegak otorisasi yang sebenarnya: satu handler baru yang lupa
	// memanggil requireSudo tidak boleh cukup untuk membuka jalur root.
	helperproto.CmdWGServerInfo: true,
	helperproto.CmdWGServerInit: true,
	helperproto.CmdWGPeerAdd:    true,
	helperproto.CmdWGPeerDelete: true,
	helperproto.CmdUpdateStatus: true,
	helperproto.CmdUpdateStart:  true,
	// Mengubah daftar printer berarti mengubah konfigurasi cupsd untuk semua
	// user mesin, jadi jelas sudo. Yang TIDAK ada di daftar ini disengaja:
	// CmdPrinterList, CmdPrintJobs, CmdPrintCancel, dan CmdPrintFile harus bisa
	// dipakai user biasa — mencetak berkas sendiri dari file manager adalah
	// alasan fitur ini ada, dan berkasnya tetap dibaca dengan hak user itu.
	helperproto.CmdPrinterAdd:     true,
	helperproto.CmdPrinterDelete:  true,
	helperproto.CmdPrinterDefault: true,
	helperproto.CmdPrinterEnable:  true,
	helperproto.CmdPrinterDevices: true,
	helperproto.CmdPrinterModels:  true,
	// Deteksi memindai perangkat, dan pemasangan driver menjalankan apt sebagai
	// root — keduanya jelas bukan aksi user biasa.
	helperproto.CmdPrinterDeteksi:       true,
	helperproto.CmdPrinterDriverInstall: true,
}

func errRequiresSudo() error {
	return &helperErr{code: helperproto.ErrRequiresSudo, msg: "Aksi ini butuh akses sudo"}
}

func errInvalid(format string, a ...any) error {
	return &helperErr{code: helperproto.ErrInvalid, msg: fmt.Sprintf(format, a...)}
}

// errKode: sama seperti errInvalid, tapi menyertakan kode yang bisa
// diterjemahkan frontend. Dipakai untuk kegagalan yang benar-benar dibaca user.
func errKode(kodeUI, format string, a ...any) error {
	e := &helperErr{code: helperproto.ErrInvalid, msg: fmt.Sprintf(format, a...), kodeUI: kodeUI}
	for _, x := range a {
		e.params = append(e.params, fmt.Sprint(x))
	}
	return e
}

func errDenied(format string, a ...any) error {
	return &helperErr{code: helperproto.ErrDenied, msg: fmt.Sprintf(format, a...)}
}

// checkPath memastikan user boleh menyentuh path ini.
// Non-sudoer dijail ke home directory-nya; sudoer bebas (kernel tetap jadi
// penegak akhir lewat worker yang berjalan sebagai user tersebut).
func (s *Server) checkPath(u *userInfo, path string) (string, error) {
	if path == "" {
		return "", errKode(helperproto.ErrPathTidakValid, "path kosong")
	}
	clean := filepath.Clean(path)
	if !filepath.IsAbs(clean) {
		return "", errKode(helperproto.ErrPathTidakValid, "path harus absolut")
	}
	if u.Sudo {
		return clean, nil
	}
	home := filepath.Clean(u.Home)
	if home == "" || home == "/" {
		return "", errDenied("home directory tidak valid untuk user %s", u.Name)
	}
	if !dalamHome(clean, home) {
		return "", &helperErr{code: helperproto.ErrDenied, msg: fmt.Sprintf("akses ke %s butuh sudo (di luar home directory)", clean), kodeUI: helperproto.ErrDiLuarHome, params: []string{clean}}
	}
	// Pemeriksaan di atas hanya leksikal. Symlink di dalam home yang menunjuk
	// keluar (mis. ln -s /etc ~/pintasan) akan lolos, dan panel ikut
	// menampilkan isi di luar jail. Karena itu path diresolusi dulu — bagian
	// yang belum ada (target mkdir/upload) dipangkas sampai ancestor yang
	// benar-benar ada, lalu hasilnya dicek ulang.
	if nyata, err := resolusiAncestor(clean); err == nil && !dalamHome(nyata, home) {
		return "", &helperErr{code: helperproto.ErrDenied, msg: fmt.Sprintf("akses ke %s butuh sudo (symlink menunjuk keluar home directory)", clean), kodeUI: helperproto.ErrSymlinkKeluar, params: []string{clean}}
	}
	return clean, nil
}

// dalamHome: path sama dengan home atau berada di dalamnya. Perbandingan
// memakai batas separator, jadi "/home/ani" tidak ikut membuka "/home/anita".
func dalamHome(path, home string) bool {
	return path == home || strings.HasPrefix(path, home+string(filepath.Separator))
}

// resolusiAncestor mengembalikan path setelah symlink diikuti. Untuk path yang
// belum ada, ancestor terdekat yang ada yang diresolusi lalu sisa nama
// ditempel kembali — `EvalSymlinks` sendiri gagal untuk path yang belum ada.
func resolusiAncestor(path string) (string, error) {
	sisa := ""
	cur := path
	for {
		if nyata, err := filepath.EvalSymlinks(cur); err == nil {
			return filepath.Join(nyata, sisa), nil
		}
		induk := filepath.Dir(cur)
		if induk == cur {
			return "", os.ErrNotExist
		}
		sisa = filepath.Join(filepath.Base(cur), sisa)
		cur = induk
	}
}
