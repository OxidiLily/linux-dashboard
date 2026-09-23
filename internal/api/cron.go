package api

import (
	"net/http"
	"strings"

	"linux-dashboard/OxidiLily/internal/helperproto"
)

// Cronjob: crontab milik akun yang sedang login.
//
// Halaman ini TIDAK butuh sudo dan memang tidak boleh: yang dikelola adalah
// jadwal milik user sendiri, dan helper menjalankan crontab sebagai identitas
// akun itu — jadi tidak ada jalur dari sini menuju crontab user lain atau
// crontab sistem. Panelnya sendiri sudah dibatasi PAM saat login; yang
// melindungi crontab orang lain adalah kernel, bukan requireSudo.

type cronBody struct {
	Isi string `json:"isi"`
	// Previous WAJIB ada. Ia adalah isi crontab terakhir yang dilihat UI, dan
	// helper menolak permintaan tanpa field ini: penyimpanan yang menimpa
	// jadwal tanpa pernah membacanya lebih dulu adalah cara kehilangan
	// pekerjaan orang lain tanpa jejak.
	Previous *string `json:"previous"`
}

func (s *Server) handleCronGet(w http.ResponseWriter, r *http.Request) {
	var hasil helperproto.CronHasil
	if err := s.helper.Call(helperproto.CmdCronGet, sessionFrom(r).HelperToken, nil, &hasil); err != nil {
		writeHelperErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, hasil)
}

func (s *Server) handleCronSave(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r)
	var body cronBody
	if err := decodeBody(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	// Batas yang sama diperiksa di sini juga supaya permintaan yang jelas
	// terlalu besar tidak perlu menempuh perjalanan ke helper dulu. Helper
	// tetap penentu akhirnya — angka batasnya hidup di satu tempat
	// (helperproto.CronMaxBytes), bukan disalin ke sini.
	if len(body.Isi) > helperproto.CronMaxBytes {
		writeErrKode(w, http.StatusBadRequest, helperproto.ErrNilaiTidakValid,
			"isi crontab terlalu besar", "cron")
		return
	}
	var hasil helperproto.CronHasil
	if err := s.helper.Call(helperproto.CmdCronPut, sess.HelperToken,
		helperproto.CronArgs{Isi: body.Isi, Previous: body.Previous}, &hasil); err != nil {
		writeHelperErr(w, err)
		return
	}
	// Perubahan jadwal dicatat seperti aksi lain di panel: satu baris yang
	// menautkan akun ke waktu penyimpanannya. Isi crontabnya TIDAK ikut
	// dicatat — ia bisa memuat token atau path privat user, dan log ini
	// dibaca halaman lain.
	s.store.LogActivity(sess.Username, "cron_save", "ubah crontab",
		map[string]any{"baris": jumlahBaris(hasil.Isi)}, clientIP(r))
	writeJSON(w, http.StatusOK, hasil)
}

// jumlahBaris dipakai hanya untuk keterangan di activity log.
func jumlahBaris(s string) int {
	if s == "" {
		return 0
	}
	n := strings.Count(s, "\n")
	if !strings.HasSuffix(s, "\n") {
		n++
	}
	return n
}
