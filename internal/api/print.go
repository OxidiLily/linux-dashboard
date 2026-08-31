package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"linux-dashboard/OxidiLily/internal/helperproto"
)

// ---- Print server (CUPS) ----
//
// Pembagian sudo di sini sengaja tidak seragam. Mengubah daftar printer adalah
// konfigurasi mesin untuk semua orang, jadi butuh sudo. Melihat printer yang
// tersedia, melihat antrean, dan mencetak berkas sendiri TIDAK: itu justru
// alasan fitur ini ada, dan helper tetap membaca berkasnya dengan hak user
// yang bersangkutan. Penegak sebenarnya ada di helper (sudoRequired) — gate di
// sini hanya supaya UI mendapat 403 yang jelas, bukan error mentah.

func (s *Server) handlePrinterList(w http.ResponseWriter, r *http.Request) {
	var out []helperproto.Printer
	if err := s.helper.Call(helperproto.CmdPrinterList, sessionFrom(r).Username, nil, &out); err != nil {
		writeHelperErr(w, err)
		return
	}
	if out == nil {
		out = []helperproto.Printer{}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handlePrinterAdd(w http.ResponseWriter, r *http.Request) {
	if !requireSudo(w, r) {
		return
	}
	sess := sessionFrom(r)
	var a helperproto.PrinterAddArgs
	if err := decodeBody(r, &a); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.helper.Call(helperproto.CmdPrinterAdd, sess.Username, a, nil); err != nil {
		writeHelperErr(w, err)
		return
	}
	s.store.LogActivity(sess.Username, "printer_add", "tambah printer",
		map[string]any{"name": a.Name, "uri": a.URI}, clientIP(r))
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handlePrinterDelete(w http.ResponseWriter, r *http.Request) {
	if !requireSudo(w, r) {
		return
	}
	sess := sessionFrom(r)
	name := chi.URLParam(r, "name")
	if err := s.helper.Call(helperproto.CmdPrinterDelete, sess.Username,
		helperproto.PrinterNameArgs{Name: name}, nil); err != nil {
		writeHelperErr(w, err)
		return
	}
	s.store.LogActivity(sess.Username, "printer_delete", "hapus printer",
		map[string]any{"name": name}, clientIP(r))
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handlePrinterDefault(w http.ResponseWriter, r *http.Request) {
	if !requireSudo(w, r) {
		return
	}
	sess := sessionFrom(r)
	name := chi.URLParam(r, "name")
	if err := s.helper.Call(helperproto.CmdPrinterDefault, sess.Username,
		helperproto.PrinterNameArgs{Name: name}, nil); err != nil {
		writeHelperErr(w, err)
		return
	}
	s.store.LogActivity(sess.Username, "printer_default", "set printer default",
		map[string]any{"name": name}, clientIP(r))
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handlePrinterEnable(w http.ResponseWriter, r *http.Request) {
	if !requireSudo(w, r) {
		return
	}
	sess := sessionFrom(r)
	var a helperproto.PrinterNameArgs
	if err := decodeBody(r, &a); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	a.Name = chi.URLParam(r, "name")
	if err := s.helper.Call(helperproto.CmdPrinterEnable, sess.Username, a, nil); err != nil {
		writeHelperErr(w, err)
		return
	}
	aksi := "printer_disable"
	if a.Enable {
		aksi = "printer_enable"
	}
	s.store.LogActivity(sess.Username, aksi, "ubah status antrean printer",
		map[string]any{"name": a.Name, "enable": a.Enable}, clientIP(r))
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handlePrinterDevices(w http.ResponseWriter, r *http.Request) {
	if !requireSudo(w, r) {
		return
	}
	var out []helperproto.PrinterDevice
	if err := s.helper.Call(helperproto.CmdPrinterDevices, sessionFrom(r).Username, nil, &out); err != nil {
		writeHelperErr(w, err)
		return
	}
	if out == nil {
		out = []helperproto.PrinterDevice{}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handlePrinterModels(w http.ResponseWriter, r *http.Request) {
	if !requireSudo(w, r) {
		return
	}
	var out []helperproto.PrinterModel
	if err := s.helper.Call(helperproto.CmdPrinterModels, sessionFrom(r).Username, nil, &out); err != nil {
		writeHelperErr(w, err)
		return
	}
	if out == nil {
		out = []helperproto.PrinterModel{}
	}
	writeJSON(w, http.StatusOK, out)
}

// handlePrinterDeteksi memindai perangkat sekaligus melaporkan driver mana yang
// masih kurang, supaya halaman print server bisa menuntun sampai printer benar
// benar siap dipakai — bukan sekadar menampilkan daftar perangkat.
func (s *Server) handlePrinterDeteksi(w http.ResponseWriter, r *http.Request) {
	if !requireSudo(w, r) {
		return
	}
	var out []helperproto.PrinterDeteksi
	if err := s.helper.Call(helperproto.CmdPrinterDeteksi, sessionFrom(r).Username, nil, &out); err != nil {
		writeHelperErr(w, err)
		return
	}
	if out == nil {
		out = []helperproto.PrinterDeteksi{}
	}
	writeJSON(w, http.StatusOK, out)
}

// handlePrinterDriverInstall memasang paket driver. Sinkron seperti pemasangan
// komponen: apt bisa berjalan puluhan detik, dan frontend menunggu dengan
// keadaan loading, bukan dengan polling.
func (s *Server) handlePrinterDriverInstall(w http.ResponseWriter, r *http.Request) {
	if !requireSudo(w, r) {
		return
	}
	sess := sessionFrom(r)
	var a helperproto.DriverInstallArgs
	if err := decodeBody(r, &a); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.helper.Call(helperproto.CmdPrinterDriverInstall, sess.Username, a, nil); err != nil {
		writeHelperErr(w, err)
		return
	}
	s.store.LogActivity(sess.Username, "printer_driver_install", "pasang driver printer",
		map[string]any{"vendor": a.Vendor}, clientIP(r))
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handlePrintJobs(w http.ResponseWriter, r *http.Request) {
	var out []helperproto.PrintJob
	if err := s.helper.Call(helperproto.CmdPrintJobs, sessionFrom(r).Username, nil, &out); err != nil {
		writeHelperErr(w, err)
		return
	}
	if out == nil {
		out = []helperproto.PrintJob{}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handlePrintCancel(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r)
	id := chi.URLParam(r, "id")
	if err := s.helper.Call(helperproto.CmdPrintCancel, sess.Username,
		helperproto.PrinterNameArgs{Name: id}, nil); err != nil {
		writeHelperErr(w, err)
		return
	}
	s.store.LogActivity(sess.Username, "print_cancel", "batalkan cetakan",
		map[string]any{"job": id}, clientIP(r))
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handlePrintFile(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r)
	var a helperproto.PrintFileArgs
	if err := decodeBody(r, &a); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	var hasil helperproto.PrintFileHasil
	if err := s.helper.Call(helperproto.CmdPrintFile, sess.Username, a, &hasil); err != nil {
		writeHelperErr(w, err)
		return
	}
	s.store.LogActivity(sess.Username, "print_file", "cetak berkas",
		map[string]any{"path": a.Path, "printer": hasil.Printer, "job": hasil.JobID}, clientIP(r))
	writeJSON(w, http.StatusOK, hasil)
}
