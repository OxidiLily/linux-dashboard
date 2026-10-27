package helper

// Wake-on-LAN: status WOL per interface (ethtool), pengaturan persisten, dan
// pengiriman magic packet (etherwake).
//
// Persistensi dibutuhkan karena flag `ethtool -s … wol` hilang setiap reboot —
// tanpa unit yang menerapkannya ulang, tombol di panel terlihat bekerja sampai
// server dihidupkan kembali. State sebenarnya ada di wol.json (map interface →
// aktif); unit systemd-nya dihasilkan dari state itu, jadi unit adalah turunan
// yang boleh ditulis ulang kapan pun, bukan sumber kebenaran.

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"linux-dashboard/OxidiLily/internal/helperproto"
)

const (
	wolStatePath = "/var/lib/linux-dashboard-helper/wol.json"
	wolUnitPath  = "/etc/systemd/system/linux-dashboard-wol.service"
)

var (
	// Nama interface divalidasi ketat: dipakai sebagai argumen ethtool dan
	// ditulis ke unit systemd. Karakter di luar pola ini tidak mungkin muncul
	// di nama interface Linux yang sah.
	wolNamaRegex = regexp.MustCompile(`^[a-zA-Z0-9_.-]{1,15}$`)
	wolMACRegex  = regexp.MustCompile(`^([0-9a-fA-F]{2}:){5}[0-9a-fA-F]{2}$`)
)

// parseWakeOn membaca keluaran `ethtool <iface>`:
//
//	Supports Wake-on: pumbg
//	Wake-on: g
//
// dikenal: baris WOL-nya ada (interface fisik; interface virtual tidak
// mengeluarkan baris ini sama sekali). didukung: nilai Supports bukan "d".
// aktif: Wake-on mengandung "g" (magic packet).
func parseWakeOn(keluaran string) (dikenal, didukung, aktif bool) {
	for _, line := range strings.Split(keluaran, "\n") {
		line = strings.TrimSpace(line)
		if after, ok := strings.CutPrefix(line, "Supports Wake-on:"); ok {
			dikenal = true
			v := strings.TrimSpace(after)
			didukung = v != "" && v != "d"
		}
		if after, ok := strings.CutPrefix(line, "Wake-on:"); ok {
			aktif = strings.Contains(strings.TrimSpace(after), "g")
		}
	}
	return
}

// wolStatus mendaftar interface /sys/class/net (selain lo) yang mengenali
// WOL. Interface virtual dilewati sepenuhnya — menampilkannya sebagai
// "tidak didukung" hanya membuat daftar jadi derau (docker0, veth*, …).
func wolStatus() helperproto.WolStatus {
	st := helperproto.WolStatus{Ifaces: []helperproto.WolIface{}}
	ethtool, errEttool := exec.LookPath("ethtool")
	st.Installed = errEttool == nil
	_, err := exec.LookPath("etherwake")
	st.SendReady = err == nil
	entries, err := os.ReadDir("/sys/class/net")
	if err != nil {
		return st
	}
	for _, e := range entries {
		name := e.Name()
		if name == "lo" || !wolNamaRegex.MatchString(name) {
			continue
		}
		mac, _ := os.ReadFile("/sys/class/net/" + name + "/address")
		wi := helperproto.WolIface{Name: name, MAC: strings.TrimSpace(string(mac))}
		if st.Installed {
			res, _ := run(ethtool, name)
			dikenal, didukung, aktif := parseWakeOn(res.Stdout)
			if !dikenal {
				continue
			}
			wi.Supported, wi.Enabled = didukung, aktif
		}
		st.Ifaces = append(st.Ifaces, wi)
	}
	return st
}

// wolState membaca map interface → aktif yang sudah dipersistensikan.
func wolState() map[string]bool {
	isi := map[string]bool{}
	b, err := os.ReadFile(wolStatePath)
	if err != nil {
		return isi
	}
	_ = json.Unmarshal(b, &isi)
	return isi
}

// unitWol menghasilkan isi unit oneshot dari state. Urutan interface
// diurutkan supaya outputnya deterministik (mudah diuji dan diff-nya stabil).
func unitWol(isi map[string]bool, ethtoolPath string) string {
	var b strings.Builder
	b.WriteString("[Unit]\nDescription=Wake-on-LAN settings (linux-dashboard)\nAfter=network-pre.target\nWants=network-pre.target\n\n[Service]\nType=oneshot\n")
	for _, name := range slices.Sorted(maps.Keys(isi)) {
		flag := "d"
		if isi[name] {
			flag = "g"
		}
		fmt.Fprintf(&b, "ExecStart=%s -s %s wol %s\n", ethtoolPath, name, flag)
	}
	b.WriteString("\n[Install]\nWantedBy=multi-user.target\n")
	return b.String()
}

// wolSet mengubah flag WOL satu interface dan menyimpannya agar diterapkan
// ulang saat boot lewat unit systemd linux-dashboard-wol.service.
func wolSet(name string, enabled bool) error {
	if !wolNamaRegex.MatchString(name) {
		return errors.New("nama interface tidak valid: " + name)
	}
	if _, err := os.Stat("/sys/class/net/" + name); err != nil {
		return errors.New("interface tidak ditemukan: " + name)
	}
	ethtool, err := exec.LookPath("ethtool")
	if err != nil {
		return errors.New("ethtool belum terpasang — pasang komponen wakeonlan di halaman Components")
	}
	// Interface virtual tidak mengenali WOL; tolak sebelum menulis state
	// yang tidak akan pernah bisa diterapkan.
	res, err := run(ethtool, name)
	dikenal, didukung, _ := parseWakeOn(res.Stdout)
	if err != nil || !dikenal {
		return errors.New("interface " + name + " tidak mendukung Wake-on-LAN")
	}
	if !didukung {
		return errors.New("Wake-on-LAN tidak didukung perangkat pada " + name)
	}
	flag := "d"
	if enabled {
		flag = "g"
	}
	if _, err := run(ethtool, "-s", name, "wol", flag); err != nil {
		return err
	}

	isi := wolState()
	isi[name] = enabled
	b, err := json.Marshal(isi)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(wolStatePath), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(wolStatePath, b, 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(wolUnitPath, []byte(unitWol(isi, ethtool)), 0o644); err != nil {
		return err
	}
	if _, err := run("systemctl", "daemon-reload"); err != nil {
		return err
	}
	_, err = run("systemctl", "enable", "linux-dashboard-wol.service")
	return err
}

// wolSend mengirim magic packet ke MAC tujuan lewat etherwake.
func wolSend(mac, iface string) error {
	if !wolMACRegex.MatchString(mac) {
		return errors.New("MAC tidak valid: " + mac)
	}
	if iface != "" && !wolNamaRegex.MatchString(iface) {
		return errors.New("nama interface tidak valid: " + iface)
	}
	etherwake, err := exec.LookPath("etherwake")
	if err != nil {
		return errors.New("etherwake belum terpasang — pasang komponen wakeonlan di halaman Components")
	}
	args := []string{}
	if iface != "" {
		args = append(args, "-i", iface)
	}
	args = append(args, mac)
	_, err = run(etherwake, args...)
	return err
}
