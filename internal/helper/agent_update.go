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
		if err != nil || string(shim) != "#!/bin/sh\nexec "+launcher+" \"$@\"\n" ||
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

func cekVersiBaruAgen(name string, u *userInfo, path string) string {
	local, err := jalankanAgen(u, 8*time.Second, path, "--version")
	if err != nil {
		return ""
	}
	if name == "hermes" {
		// Versi checkout dapat tetap sama walau sudah ratusan commit tertinggal.
		if !strings.Contains(local, "Install method: git") {
			return ""
		}
		out, err := jalankanAgen(u, 25*time.Second, path, "update", "--check")
		if err == nil && strings.Contains(out, "Update available:") {
			return "commits"
		}
		return ""
	}
	current := versiSemver(local)
	if current == "" {
		return ""
	}
	var latest string
	switch name {
	case "claude-code":
		// Jalur native resmi: symlink .local/bin ke .local/share/claude.
		real, err := filepath.EvalSymlinks(path)
		if err != nil || !strings.HasPrefix(real, filepath.Join(u.Home, ".local/share/claude/versions")+"/") {
			return ""
		}
		latest = ambilVersiURL("https://storage.googleapis.com/claude-code-dist-86c565f3-f756-42ad-8dfa-d59b1c096819/claude-code-releases/latest")
	case "codex":
		// Installer resmi meletakkan standalone binary di .local/bin.
		if filepath.Dir(path) != filepath.Join(u.Home, ".local/bin") {
			return ""
		}
		latest = ambilVersiURL("https://registry.npmjs.org/@openai%2fcodex/latest")
	case "opencode":
		if filepath.Dir(path) != filepath.Join(u.Home, ".opencode/bin") {
			return ""
		}
		latest = ambilVersiURL("https://registry.npmjs.org/opencode-ai/latest")
	case "openclaw":
		// Status native memiliki bentuk JSON yang berubah antarrilis;
		// hanya tawarkan update saat versi kanal stable dapat diverifikasi.
		if filepath.Dir(path) != filepath.Join(u.Home, ".npm-global/bin") {
			return ""
		}
		latest = ambilVersiURL("https://registry.npmjs.org/openclaw/latest")
	}
	if bandingVersi(current, latest) {
		return latest
	}
	return ""
}

func updateAgen(name string, u *userInfo) error {
	if !komponenAgenAI(name) {
		return errInvalid("agent tidak dikenal")
	}
	path, ok := agenMilikUser(name, u)
	if !ok {
		return errInvalid("agent %s bukan instalasi milik user panel; pembaruan otomatis ditolak", name)
	}
	target := versiBaruAgen(name, u)
	if target == "" {
		return errInvalid("update %s tidak terverifikasi; segarkan halaman Components", name)
	}
	before, err := jalankanAgen(u, 8*time.Second, path, "--version")
	if err != nil {
		return errInvalid("versi %s tidak dapat dibaca: %v", name, err)
	}
	if err := mulaiProgres(name, "install"); err != nil {
		return err
	}
	defer selesaiProgres()
	defer lupakanCacheUpdates()
	// Jangan gunakan target cache lama saat update tertunda lama di tab browser.
	if cekVersiBaruAgen(name, u, path) == "" {
		return errInvalid("update %s sudah tidak tersedia atau kanal tidak dapat diperiksa", name)
	}
	tahapBaru("memperbarui " + name + " sebagai " + u.Name)
	var args []string
	switch name {
	case "hermes":
		args = []string{"update"}
	case "claude-code":
		args = []string{"update"}
	case "opencode":
		args = []string{"upgrade"}
	case "openclaw":
		args = []string{"update", "--yes", "--no-restart"}
	case "codex":
		// Standalone installer adalah jalur upgrade resmi untuk binary ini.
		if err := installAgenResmi(name, components[name].Binary, u); err != nil {
			return err
		}
		args = nil
	default:
		return errInvalid("agent tidak dikenal")
	}
	if len(args) != 0 {
		_, err := jalankanAgen(u, batasInstallAgen, path, args...)
		if err != nil {
			// Keluaran CLI dapat mengandung path/token dari konfigurasi user.
			return errInvalid("update %s gagal: %v", name, err)
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
		return errInvalid("update %s selesai tetapi versi baru tidak dapat diverifikasi", name)
	}
	if name == "hermes" {
		if cekVersiBaruAgen(name, u, path) != "" {
			return errInvalid("update %s selesai tetapi checkout masih tertinggal", name)
		}
	} else if !bandingVersi(versiSemver(before), versiSemver(baru)) || bandingVersi(versiSemver(baru), target) {
		return errInvalid("update %s selesai tetapi versi target %s belum terpasang", name, target)
	}
	return nil
}
