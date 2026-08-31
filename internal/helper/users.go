package helper

import (
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"syscall"

	"linux-dashboard/OxidiLily/internal/helperproto"
)

var usernameRe = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)

// minLoginUID memisahkan akun manusia dari akun sistem (konvensi Debian/Ubuntu
// di /etc/login.defs: UID_MIN 1000).
const minLoginUID = 1000

// nobody (65534) & nogroup (65535) adalah akun semu kernel/NFS, bukan akun
// manusia — UID-nya di atas ambang login jadi harus dikecualikan terpisah.
const nobodyUID = 65534

func listLinuxUsers() ([]helperproto.LinuxUser, error) {
	b, err := os.ReadFile("/etc/passwd")
	if err != nil {
		return nil, err
	}
	locked := lockedUsers()
	var out []helperproto.LinuxUser
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Split(line, ":")
		if len(f) < 7 {
			continue
		}
		uid, err := strconv.Atoi(f[2])
		if err != nil {
			continue
		}
		if uid != 0 && (uid < minLoginUID || uid >= nobodyUID) {
			continue
		}
		// Shell TIDAK dipakai sebagai filter di sini. Akun sistem sudah
		// tersaring oleh ambang UID di atas; menyaring nologin juga membuat
		// user biasa yang shell-nya baru diset nologin lewat panel langsung
		// hilang dari daftar — tidak bisa dikembalikan atau dihapus dari UI
		// (PRD 5.5.1: panel menampilkan SEMUA user Linux di device).
		gid, _ := strconv.Atoi(f[3])
		lu := helperproto.LinuxUser{
			Username: f[0], UID: uid, GID: gid,
			Comment: strings.TrimRight(f[4], ","), Home: f[5], Shell: f[6],
			Locked: locked[f[0]],
		}
		if u, err := user.Lookup(f[0]); err == nil {
			if gids, err := u.GroupIds(); err == nil {
				for _, g := range gids {
					if grp, err := user.LookupGroupId(g); err == nil {
						lu.Groups = append(lu.Groups, grp.Name)
					}
				}
			}
		}
		out = append(out, lu)
	}
	slices.SortFunc(out, func(a, b helperproto.LinuxUser) int { return a.UID - b.UID })
	return out, nil
}

// lockedUsers membaca /etc/shadow — field password diawali "!" atau "*"
// menandakan akun terkunci.
func lockedUsers() map[string]bool {
	out := map[string]bool{}
	b, err := os.ReadFile("/etc/shadow")
	if err != nil {
		return out
	}
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Split(line, ":")
		if len(f) < 2 {
			continue
		}
		out[f[0]] = strings.HasPrefix(f[1], "!") || strings.HasPrefix(f[1], "*")
	}
	return out
}

func validShell(shell string) error {
	if shell == "" {
		return nil
	}
	b, err := os.ReadFile("/etc/shells")
	if err != nil {
		return nil
	}
	for _, line := range strings.Split(string(b), "\n") {
		if strings.TrimSpace(line) == shell {
			return nil
		}
	}
	if isServiceAccount(shell) {
		return nil // nologin sah untuk mengunci akun
	}
	return errInvalid("shell %q tidak terdaftar di /etc/shells", shell)
}

func validGroups(groups []string) error {
	for _, g := range groups {
		if _, err := user.LookupGroup(g); err != nil {
			return errInvalid("grup %q tidak ada", g)
		}
	}
	return nil
}

func createLinuxUser(args helperproto.UserCreateArgs) error {
	if !usernameRe.MatchString(args.Username) {
		return errInvalid("username tidak valid (huruf kecil, angka, - dan _)")
	}
	if _, err := user.Lookup(args.Username); err == nil {
		return errInvalid("user %q sudah ada", args.Username)
	}
	if err := validShell(args.Shell); err != nil {
		return err
	}
	if err := validGroups(args.Groups); err != nil {
		return err
	}
	cmd := []string{}
	if args.MakeHome {
		cmd = append(cmd, "-m")
	}
	if args.Shell != "" {
		cmd = append(cmd, "-s", args.Shell)
	}
	if args.Comment != "" {
		cmd = append(cmd, "-c", sanitizeComment(args.Comment))
	}
	if len(args.Groups) > 0 {
		cmd = append(cmd, "-G", strings.Join(args.Groups, ","))
	}
	cmd = append(cmd, args.Username)
	if _, err := run("useradd", cmd...); err != nil {
		return err
	}
	if args.MakeHome {
		if err := claimHome(args.Username); err != nil {
			// Akun tanpa home yang bisa diakses = akun rusak; jangan disisakan.
			_, _ = run("userdel", args.Username)
			return err
		}
	}
	if args.Password != "" {
		if err := chauthtok(args.Username, args.Password); err != nil {
			// Rollback supaya tidak meninggalkan akun tanpa password yang bisa dipakai.
			_, _ = run("userdel", "-r", args.Username)
			return err
		}
	}
	return nil
}

// claimHome memastikan home directory benar-benar milik user yang baru dibuat.
//
// `useradd -m` TIDAK menyentuh direktori yang sudah ada: kalau user dengan nama
// sama pernah dihapus tanpa ikut membuang home-nya (userdel menolak menghapus
// direktori yang bukan milik user itu), direktori lama dipakai ulang apa adanya
// beserta UID pemilik lamanya. UID user baru hampir selalu berbeda, jadi akun
// baru langsung tertolak dari home-nya sendiri — muncul sebagai
// "open /home/<user>: permission denied" begitu File Manager dibuka.
func claimHome(username string) error {
	u, err := lookupUser(username)
	if err != nil {
		return err
	}
	home := filepath.Clean(u.Home)
	if home == "" || home == "/" || filepath.Dir(home) == home {
		return errInvalid("home directory %q tidak valid untuk user %s", u.Home, username)
	}
	fi, err := os.Stat(home)
	if os.IsNotExist(err) {
		return nil // useradd tanpa -m, atau home memang belum dibuat
	}
	if err != nil {
		return err
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok || (int(st.Uid) == u.UID && int(st.Gid) == u.GID) {
		return nil
	}
	// Direktori sisa boleh diambil alih; direktori milik akun yang MASIH ADA
	// tidak — itu akan menyerahkan data user lain ke akun baru.
	if owner, err := user.LookupId(strconv.Itoa(int(st.Uid))); err == nil && owner.Username != username {
		return errInvalid("home %s masih milik user %s — pindahkan atau hapus dulu", home, owner.Username)
	}
	if err := filepathWalkChown(home, u.UID, u.GID); err != nil {
		return err
	}
	return os.Chmod(home, 0o750)
}

// sanitizeComment membuang karakter yang merusak format /etc/passwd.
func sanitizeComment(s string) string {
	return strings.NewReplacer(":", " ", "\n", " ", "\r", " ", ",", " ").Replace(s)
}

func modifyLinuxUser(args helperproto.UserModifyArgs) error {
	if !usernameRe.MatchString(args.Username) {
		return errInvalid("username tidak valid")
	}
	u, err := lookupUser(args.Username)
	if err != nil {
		return errInvalid("user %q tidak ditemukan", args.Username)
	}
	if err := validShell(args.Shell); err != nil {
		return err
	}
	if err := validGroups(args.Groups); err != nil {
		return err
	}
	var cmd []string
	if args.Shell != "" && args.Shell != u.Shell {
		cmd = append(cmd, "-s", args.Shell)
	}
	if args.Comment != "" {
		cmd = append(cmd, "-c", sanitizeComment(args.Comment))
	}
	if args.Groups != nil {
		// -G tanpa -a mengganti seluruh keanggotaan grup sekunder.
		cmd = append(cmd, "-G", strings.Join(args.Groups, ","))
	}
	if len(cmd) > 0 {
		cmd = append(cmd, args.Username)
		if _, err := run("usermod", cmd...); err != nil {
			return err
		}
	}
	if args.Lock != nil {
		if u.UID == 0 {
			return errInvalid("akun root tidak boleh dikunci dari panel")
		}
		flag := "-U"
		if *args.Lock {
			flag = "-L"
		}
		if _, err := run("usermod", flag, args.Username); err != nil {
			return err
		}
	}
	return nil
}

func deleteLinuxUser(args helperproto.UserDeleteArgs) error {
	if !usernameRe.MatchString(args.Username) {
		return errInvalid("username tidak valid")
	}
	u, err := lookupUser(args.Username)
	if err != nil {
		return errInvalid("user %q tidak ditemukan", args.Username)
	}
	if u.UID == 0 {
		return errInvalid("akun root tidak boleh dihapus")
	}
	cmd := []string{}
	if args.RemoveHome {
		cmd = append(cmd, "-r")
	}
	cmd = append(cmd, args.Username)
	if _, err = run("userdel", cmd...); err != nil {
		// userdel exit != 0 juga terjadi untuk peringatan non-fatal (mail spool
		// tidak ada, home dir bukan milik user) padahal akun sudah terhapus.
		if _, lookErr := lookupUser(args.Username); lookErr != nil {
			return nil
		}
		return err
	}
	return nil
}
