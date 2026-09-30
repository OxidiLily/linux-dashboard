package helper

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"linux-dashboard/OxidiLily/internal/helperproto"
)

// Update hanya menyentuh binary yang berada di HOME peminta. Instalasi
// system-wide, channel khusus, dan pemilik paket yang tak dikenal dibiarkan.
var versiAgenRE = regexp.MustCompile(`\b[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?\b`)

func versiSemver(s string) string { return versiAgenRE.FindString(s) }

func bandingVersi(a, b string) bool {
	// Prerelease tidak dibandingkan dengan stable; menghindari downgrade.
	if !versiAgenRE.MatchString(a) || !versiAgenRE.MatchString(b) ||
		strings.Contains(a, "-") || strings.Contains(b, "-") {
		return false
	}
	var x, y [3]int
	if _, e := fmt.Sscanf(a, "%d.%d.%d", &x[0], &x[1], &x[2]); e != nil {
		return false
	}
	if _, e := fmt.Sscanf(b, "%d.%d.%d", &y[0], &y[1], &y[2]); e != nil {
		return false
	}
	for i := range x {
		if y[i] != x[i] {
			return y[i] > x[i]
		}
	}
	return false
}

func agenMilikUser(name string, u *userInfo) (string, bool) {
	if u == nil || u.Home == "" {
		return "", false
	}
	c := components[name]
	if c == nil || !komponenAgenAI(name) {
		return "", false
	}
	p, ok := cariBinerAgenDiRumah(c.Binary, u.Home)
	if !ok || pemilikBerkas(p) != u.UID {
		return "", false
	}
	// Symlink milik user menuju executable di luar HOME bukan instalasi user.
	real, err := filepath.EvalSymlinks(p)
	if err != nil {
		return "", false
	}
	home, err := filepath.EvalSymlinks(u.Home)
	if err != nil || !strings.HasPrefix(real, home+string(os.PathSeparator)) || pemilikBerkas(real) != u.UID {
		return "", false
	}
	// Tolak instalasi yang tidak dikenali metodenya: updater vendor bisa
	// mengganti executable lain atau beroperasi pada prefix yang keliru.
	switch name {
	case "hermes":
		// Installer resmi menaruh shim shell dua baris di ~/.local/bin.
		// Terima hanya shim yang menunjuk launcher checkout milik user.
		launcher := filepath.Join(u.Home, ".hermes/hermes-agent/.hermes/bin/hermes")
		shim, err := os.ReadFile(real)
		if err != nil || !strings.Contains(string(shim), launcher) ||
			!bisaDieksekusi(launcher) || pemilikBerkas(launcher) != u.UID {
			return "", false
		}
	case "claude-code":
		if !strings.HasPrefix(real, filepath.Join(u.Home, ".local/share/claude/versions")+"/") {
			return "", false
		}
	case "codex":
		if filepath.Dir(p) != filepath.Join(u.Home, ".local/bin") || filepath.Ext(real) == ".js" {
			return "", false
		}
		if fi, err := os.Stat(real); err != nil || fi.Size() < 1024*1024 {
			return "", false // standalone native binary, bukan shim npm
		}
		f, err := os.Open(real)
		if err != nil {
			return "", false
		}
		var magic [4]byte
		_, err = io.ReadFull(f, magic[:])
		f.Close()
		if err != nil || string(magic[:]) != "\x7fELF" {
			return "", false
		}
	case "opencode":
		if filepath.Dir(p) != filepath.Join(u.Home, ".opencode/bin") {
			return "", false
		}
	case "openclaw":
		if filepath.Dir(p) != filepath.Join(u.Home, ".npm-global/bin") ||
			!strings.HasPrefix(real, filepath.Join(u.Home, ".npm-global/lib/node_modules/openclaw")+"/") {
			return "", false
		}
	}
	return p, true
}

func jalankanAgen(u *userInfo, timeout time.Duration, path string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Dir, cmd.Env = u.Home, envAgen(u)
	cmd.WaitDelay = 3 * time.Second
	cmd.SysProcAttr = &syscall.SysProcAttr{Credential: u.credential(), Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	return string(out), err
}

func ambilVersiURL(url string) string {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if err != nil {
		return ""
	}
	if strings.HasPrefix(string(b), "{") {
		var pkg struct {
			Version string `json:"version"`
		}
		if json.Unmarshal(b, &pkg) != nil {
			return ""
		}
		return versiSemver(pkg.Version)
	}
	return versiSemver(strings.TrimSpace(string(b)))
}

var cacheAgenBaru = struct {
	sync.Mutex
	items map[string]struct {
		version string
		at      time.Time
	}
}{items: make(map[string]struct {
	version string
	at      time.Time
})}

func versiBaruAgen(name string, u *userInfo) string {
	path, ok := agenMilikUser(name, u)
	if !ok {
		return ""
	}
	key := fmt.Sprintf("%d:%s:%s", u.UID, name, path)
	cacheAgenBaru.Lock()
	if c, ok := cacheAgenBaru.items[key]; ok && time.Since(c.at) < time.Hour {
		cacheAgenBaru.Unlock()
		return c.version
	}
	cacheAgenBaru.Unlock()
	version := cekVersiBaruAgen(name, u, path)
	cacheAgenBaru.Lock()
	cacheAgenBaru.items[key] = struct {
		version string
		at      time.Time
	}{version, time.Now()}
	cacheAgenBaru.Unlock()
	return version
}

func cekUpdateAgenLangsung(name string, u *userInfo, path string) (bool, string, error) {
	switch name {
	case "hermes":
		// Cek repository langsung.
		hermesDir := filepath.Join(u.Home, ".hermes", "hermes-agent")
		if fi, err := os.Stat(filepath.Join(hermesDir, ".git")); err == nil && fi.IsDir() {
			localShaOut, _ := jalankanAgen(u, 4*time.Second, "git", "-C", hermesDir, "rev-parse", "HEAD")
			headSha := strings.TrimSpace(localShaOut)

			// Cek remote tip via git ls-remote (cepat dan langsung dari repo remote)
			remoteOut, err := jalankanAgen(u, 6*time.Second, "git", "-C", hermesDir, "ls-remote", "origin", "refs/heads/main")
			if err == nil {
				fields := strings.Fields(remoteOut)
				if len(fields) > 0 && len(fields[0]) >= 7 {
					remoteSha := fields[0]
					if headSha != "" && remoteSha == headSha {
						return false, "", nil // Sudah versi terbaru di repo
					}
					return true, "commits", nil // Ada commit baru di remote
				}
			}

			// Fallback: rev-list lokal jika origin/main sudah pernah di-fetch
			countOut, err := jalankanAgen(u, 4*time.Second, "git", "-C", hermesDir, "rev-list", "HEAD..origin/main", "--count")
			if err == nil {
				count := strings.TrimSpace(countOut)
				if count != "" && count != "0" {
					return true, "commits", nil
				}
			}
		}

		// Fallback lewat probe --version
		local, err := jalankanAgen(u, 6*time.Second, path, "--version")
		if err == nil && (strings.Contains(local, "Update available:") || strings.Contains(local, "commits behind")) {
			return true, "commits", nil
		}
		return false, "", nil

	case "claude-code":
		local, _ := jalankanAgen(u, 6*time.Second, path, "--version")
		current := versiSemver(local)
		latest := ambilVersiURL("https://storage.googleapis.com/claude-code-dist-86c565f3-f756-42ad-8dfa-d59b1c096819/claude-code-releases/latest")
		if latest == "" {
			return false, "", errInvalid("gagal memeriksa versi terbaru claude-code dari server rilis")
		}
		if current != "" && !bandingVersi(current, latest) {
			return false, "", nil
		}
		return true, latest, nil

	case "codex":
		local, _ := jalankanAgen(u, 6*time.Second, path, "--version")
		current := versiSemver(local)
		latest := ambilVersiURL("https://registry.npmjs.org/@openai%2fcodex/latest")
		if latest == "" {
			return false, "", errInvalid("gagal memeriksa versi terbaru codex dari registry npm")
		}
		if current != "" && !bandingVersi(current, latest) {
			return false, "", nil
		}
		return true, latest, nil

	case "opencode":
		local, _ := jalankanAgen(u, 6*time.Second, path, "--version")
		current := versiSemver(local)
		latest := ambilVersiURL("https://registry.npmjs.org/opencode-ai/latest")
		if latest == "" {
			return false, "", errInvalid("gagal memeriksa versi terbaru opencode dari registry npm")
		}
		if current != "" && !bandingVersi(current, latest) {
			return false, "", nil
		}
		return true, latest, nil

	case "openclaw":
		local, _ := jalankanAgen(u, 6*time.Second, path, "--version")
		current := versiSemver(local)
		latest := ambilVersiURL("https://registry.npmjs.org/openclaw/latest")
		if latest == "" {
			return false, "", errInvalid("gagal memeriksa versi terbaru openclaw dari registry npm")
		}
		if current != "" && !bandingVersi(current, latest) {
			return false, "", nil
		}
		return true, latest, nil
	}
	return false, "", errInvalid("agent %s tidak dikenal", name)
}

func cekVersiBaruAgen(name string, u *userInfo, path string) string {
	hasUpdate, ver, err := cekUpdateAgenLangsung(name, u, path)
	if err != nil || !hasUpdate {
		return ""
	}
	return ver
}

func updateAgen(name string, u *userInfo) (*helperproto.ComponentActionResult, error) {
	if !komponenAgenAI(name) {
		return nil, errInvalid("agent tidak dikenal")
	}
	path, ok := agenMilikUser(name, u)
	if !ok {
		return nil, errInvalid("agent %s bukan instalasi milik user panel; pembaruan otomatis ditolak", name)
	}

	// Cek langsung dari repo apakah ada pembaruan
	hasUpdate, _, err := cekUpdateAgenLangsung(name, u, path)
	if err != nil {
		return nil, err
	}
	if !hasUpdate {
		return &helperproto.ComponentActionResult{
			Status:  "ok",
			Updated: false,
			Message: "Sudah di versi yang terbaru",
		}, nil
	}

	if err := mulaiProgres(name, "install"); err != nil {
		return nil, err
	}
	defer selesaiProgres()
	defer lupakanCacheUpdates()
	defer lupakanCacheKomponen()

	tahapBaru("memperbarui " + name + " sebagai " + u.Name)
	var args []string
	switch name {
	case "hermes":
		args = []string{"update", "--yes"}
	case "claude-code":
		args = []string{"update"}
	case "opencode":
		args = []string{"upgrade"}
	case "openclaw":
		args = []string{"update", "--yes", "--no-restart"}
	case "codex":
		if err := installAgenResmi(name, components[name].Binary, u); err != nil {
			return nil, err
		}
		args = nil
	default:
		return nil, errInvalid("agent tidak dikenal")
	}
	if len(args) != 0 {
		_, err := jalankanAgen(u, batasInstallAgen, path, args...)
		if err != nil {
			// Keluaran CLI dapat mengandung path/token dari konfigurasi user.
			return nil, errInvalid("update %s gagal: %v", name, err)
		}
	}
	cacheAgenBaru.Lock()
	for k := range cacheAgenBaru.items {
		if strings.Contains(k, ":"+name+":") {
			delete(cacheAgenBaru.items, k)
		}
	}
	cacheAgenBaru.Unlock()
	baru, e := jalankanAgen(u, 8*time.Second, path, "--version")
	if e != nil || baru == "" {
		return nil, errInvalid("update %s selesai tetapi versi baru tidak dapat diverifikasi", name)
	}

	return &helperproto.ComponentActionResult{
		Status:  "ok",
		Updated: true,
		Message: fmt.Sprintf("%s berhasil diperbarui", name),
	}, nil
}
