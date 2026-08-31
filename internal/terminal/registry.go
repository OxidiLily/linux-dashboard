// Package terminal mengatur kuota sesi terminal bersamaan.
package terminal

import (
	"errors"
	"os/exec"
	"runtime"
	"strings"
	"sync"
)

// MaxSessions menghitung batas sesi terminal bersamaan dari jumlah core.
//
//	cores <= 4 → 1 core : 1 user (mesin 2 core → maksimal 2 sesi; 4 core
//	             termasuk kelompok ini)
//	cores  > 4 → 1 core bisa dipakai 2 user (kapasitas 2x jumlah core)
func MaxSessions(cores int) int {
	if cores < 1 {
		cores = 1
	}
	if cores <= 4 {
		return cores
	}
	return cores * 2
}

var ErrFull = errors.New("Sesi terminal penuh, coba lagi nanti")

type Registry struct {
	mu     sync.Mutex
	active int
	max    int
}

func NewRegistry() *Registry {
	return &Registry{max: MaxSessions(runtime.NumCPU())}
}

// Acquire mengambil satu slot sesi. Ditolak eksplisit saat penuh — bukan
// dibiarkan lewat lalu membebani mesin.
func (r *Registry) Acquire() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.active >= r.max {
		return ErrFull
	}
	r.active++
	return nil
}

func (r *Registry) Release() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.active > 0 {
		r.active--
	}
}

type Capacity struct {
	Active int `json:"active"`
	Max    int `json:"max"`
	Cores  int `json:"cores"`
	// LoginUsers = jumlah akun yang sedang punya sesi login interaktif di
	// mesin (diambil dari `who -q`, fallback ke `users`). Ditampilkan di
	// header Terminal supaya user di WSL/lxc — yang "Sesi: 0" keliru
	// terlihat kosong padahal akun sendiri sedang login — punya angka
	// yang relevan. Beban panel tidak bergantung pada nilai ini: dipakai
	// murni sebagai informasi tampil.
	LoginUsers int `json:"login_users"`
}

func (r *Registry) Capacity() Capacity {
	r.mu.Lock()
	active := r.active
	r.mu.Unlock()
	return Capacity{
		Active:     active,
		Max:        MaxSessions(runtime.NumCPU()),
		Cores:      runtime.NumCPU(),
		LoginUsers: hitungLoginUsers(),
	}
}

// hitungLoginUsers menghitung akun unik yang sedang punya sesi login di
// mesin. `who -q` menampilkan daftar nama user dipisah spasi, satu entry
// per baris; bila tidak tersedia (busybox, image sangat minimal) fallback
// ke `users` (sintaks POSIX yang lebih luas dukungan).
//
// Output disikat dedupe dan whitespace supaya nilai yang sama dihitung
// sekali — `who` menuliskan user yang sama beberapa kali untuk multi-TTY.
func hitungLoginUsers() int {
	if out, err := exec.Command("who", "-q").Output(); err == nil {
		return uniqueUsers(string(out))
	}
	if out, err := exec.Command("users").Output(); err == nil {
		return uniqueUsers(string(out))
	}
	return 0
}

func uniqueUsers(s string) int {
	seen := map[string]bool{}
	for _, f := range strings.Fields(s) {
		if f == "" {
			continue
		}
		// `who -q` mencetak "# users=N" di akhir output — saat dipecah
		// oleh strings.Fields itu jadi dua field: "#" dan "users=N".
		// Keduanya bukan nama user, jadi buang dua-duanya.
		if strings.HasPrefix(f, "#") || strings.HasPrefix(f, "users=") {
			continue
		}
		seen[f] = true
	}
	return len(seen)
}
