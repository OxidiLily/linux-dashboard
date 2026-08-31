package helper

import (
	_ "embed"
	"os"
	"path/filepath"
)

// skripAgentLoop adalah supervisor sesi AI Agent, disatukan ke binary lewat
// //go:embed supaya tersedia di semua deployment tanpa folder tambahan.
//
//go:embed agent-loop.sh
var skripAgentLoop []byte

// jalurAgentLoop harus di lokasi yang bisa DIBACA DAN DIEKSEKUSI user panel
// mana pun: skripnya dijalankan di dalam PTY dengan identitas user yang
// membuka sesi, bukan root.
//
// BUKAN /run/linux-dashboard: RuntimeDirectory milik unit helper dibuat
// systemd dengan mode 0750, jadi user panel tidak bisa menembusnya sama
// sekali. BUKAN /tmp: semua user bisa menulis di sana, dan siapa pun bisa
// mendahului nama berkasnya dengan symlink lalu menunggu root menulis
// lewatnya. /usr/local/share/linux-dashboard sudah dibuat installer dengan
// mode 0755 — hanya root yang menulis, semua orang bisa membaca.
const jalurAgentLoop = "/usr/local/share/linux-dashboard/agent-loop.sh"

// pastikanAgentLoop menulis skrip supervisor ke disk kalau belum ada atau
// isinya sudah usang, lalu mengembalikan jalurnya.
//
// Ditulis saat dibutuhkan dengan membandingkan isi, bukan sekali saat start:
// binary helper yang di-update lewat tombol Update membawa versi skrip baru,
// dan berkas di disk harus ikut menyusul tanpa perlu langkah manual.
func pastikanAgentLoop() (string, error) {
	if lama, err := os.ReadFile(jalurAgentLoop); err == nil && string(lama) == string(skripAgentLoop) {
		return jalurAgentLoop, nil
	}
	if err := os.MkdirAll(filepath.Dir(jalurAgentLoop), 0o755); err != nil {
		return "", err
	}
	// 0755: dieksekusi user panel, ditulis hanya oleh root.
	if err := os.WriteFile(jalurAgentLoop, skripAgentLoop, 0o755); err != nil {
		return "", err
	}
	return jalurAgentLoop, os.Chmod(jalurAgentLoop, 0o755)
}
