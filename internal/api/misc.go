package api

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"linux-dashboard/OxidiLily/internal/helperproto"
	"linux-dashboard/OxidiLily/internal/store"
)

// ---- Bookmarks (pintasan folder File Manager, personal per user) ----

func (s *Server) handleBookmarks(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.Bookmarks(sessionFrom(r).Username)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, list)
}

type bookmarkBody struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

func (b bookmarkBody) validate() string {
	if b.Name == "" {
		return "Nama bookmark wajib diisi"
	}
	if b.Path == "" || b.Path[0] != '/' {
		return "Path bookmark harus absolut"
	}
	return ""
}

func (s *Server) handleBookmarkCreate(w http.ResponseWriter, r *http.Request) {
	var body bookmarkBody
	if err := decodeBody(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if msg := body.validate(); msg != "" {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	// Bookmark murni pintasan — tidak memberi akses apa pun. Izin folder tetap
	// ditegakkan saat File Manager membukanya lewat helper daemon.
	bm, err := s.store.AddBookmark(sessionFrom(r).Username, body.Name, body.Path)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, bm)
}

func (s *Server) handleBookmarkUpdate(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "id tidak valid")
		return
	}
	var body bookmarkBody
	if err := decodeBody(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if msg := body.validate(); msg != "" {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	if err := s.store.UpdateBookmark(sessionFrom(r).Username, id, body.Name, body.Path); err != nil {
		writeErr(w, http.StatusNotFound, "bookmark tidak ditemukan")
		return
	}
	writeJSON(w, http.StatusOK, store.Bookmark{ID: id, Name: body.Name, Path: body.Path})
}

func (s *Server) handleBookmarkDelete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "id tidak valid")
		return
	}
	if err := s.store.DeleteBookmark(sessionFrom(r).Username, id); err != nil {
		writeErr(w, http.StatusNotFound, "bookmark tidak ditemukan")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ---- Samba ----

func (s *Server) handleSambaList(w http.ResponseWriter, r *http.Request) {
	if !requireSudo(w, r) {
		return
	}
	var shares []helperproto.SambaShare
	if err := s.helper.Call(helperproto.CmdSambaList, sessionFrom(r).Username, nil, &shares); err != nil {
		writeHelperErr(w, err)
		return
	}
	if shares == nil {
		shares = []helperproto.SambaShare{}
	}
	writeJSON(w, http.StatusOK, shares)
}

func (s *Server) handleSambaSave(w http.ResponseWriter, r *http.Request) {
	if !requireSudo(w, r) {
		return
	}
	sess := sessionFrom(r)
	var share helperproto.SambaShare
	if err := decodeBody(r, &share); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.helper.Call(helperproto.CmdSambaSave, sess.Username, share, nil); err != nil {
		writeHelperErr(w, err)
		return
	}
	// Password Samba tidak pernah ikut ke log aktivitas.
	s.store.LogActivity(sess.Username, "samba_share_save", "simpan share",
		map[string]any{"name": share.Name, "path": share.Path, "writable": share.Writable}, clientIP(r))
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleSambaDelete(w http.ResponseWriter, r *http.Request) {
	if !requireSudo(w, r) {
		return
	}
	sess := sessionFrom(r)
	name := chi.URLParam(r, "name")
	if err := s.helper.Call(helperproto.CmdSambaDelete, sess.Username,
		helperproto.PathArgs{Path: name}, nil); err != nil {
		writeHelperErr(w, err)
		return
	}
	s.store.LogActivity(sess.Username, "samba_share_delete", "hapus share",
		map[string]any{"name": name}, clientIP(r))
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ---- User Samba ----
//
// Database smbpasswd terpisah dari akun Linux: user Linux yang baru dibuat
// belum bisa login ke share sampai didaftarkan di sini.

func (s *Server) handleSambaUserList(w http.ResponseWriter, r *http.Request) {
	if !requireSudo(w, r) {
		return
	}
	var users []helperproto.SambaUser
	if err := s.helper.Call(helperproto.CmdSambaUserList, sessionFrom(r).Username, nil, &users); err != nil {
		writeHelperErr(w, err)
		return
	}
	if users == nil {
		users = []helperproto.SambaUser{}
	}
	writeJSON(w, http.StatusOK, users)
}

func (s *Server) handleSambaUserSave(w http.ResponseWriter, r *http.Request) {
	if !requireSudo(w, r) {
		return
	}
	sess := sessionFrom(r)
	var args helperproto.SambaUserArgs
	if err := decodeBody(r, &args); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if name := chi.URLParam(r, "name"); name != "" {
		args.Username = name
	}
	if err := s.helper.Call(helperproto.CmdSambaUserSet, sess.Username, args, nil); err != nil {
		writeHelperErr(w, err)
		return
	}
	// Password tidak pernah ikut ke log.
	s.store.LogActivity(sess.Username, "samba_user_save", "simpan user Samba",
		map[string]any{"username": args.Username, "disable": args.Disable}, clientIP(r))
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleSambaUserDelete(w http.ResponseWriter, r *http.Request) {
	if !requireSudo(w, r) {
		return
	}
	sess := sessionFrom(r)
	name := chi.URLParam(r, "name")
	if err := s.helper.Call(helperproto.CmdSambaUserDelete, sess.Username,
		helperproto.SambaUserArgs{Username: name}, nil); err != nil {
		writeHelperErr(w, err)
		return
	}
	s.store.LogActivity(sess.Username, "samba_user_delete", "hapus user Samba",
		map[string]any{"username": name}, clientIP(r))
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ---- NFS exports ----

func (s *Server) handleNFSList(w http.ResponseWriter, r *http.Request) {
	if !requireSudo(w, r) {
		return
	}
	var out []helperproto.NFSExport
	if err := s.helper.Call(helperproto.CmdNFSList, sessionFrom(r).Username, nil, &out); err != nil {
		writeHelperErr(w, err)
		return
	}
	if out == nil {
		out = []helperproto.NFSExport{}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleNFSSave(w http.ResponseWriter, r *http.Request) {
	if !requireSudo(w, r) {
		return
	}
	sess := sessionFrom(r)
	var e helperproto.NFSExport
	if err := decodeBody(r, &e); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.helper.Call(helperproto.CmdNFSSave, sess.Username, e, nil); err != nil {
		writeHelperErr(w, err)
		return
	}
	s.store.LogActivity(sess.Username, "nfs_save", "simpan export",
		map[string]any{"path": e.Path, "clients": len(e.Clients)}, clientIP(r))
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleNFSDelete(w http.ResponseWriter, r *http.Request) {
	if !requireSudo(w, r) {
		return
	}
	sess := sessionFrom(r)
	path := r.URL.Query().Get("path")
	if err := s.helper.Call(helperproto.CmdNFSDelete, sess.Username,
		helperproto.PathArgs{Path: path}, nil); err != nil {
		writeHelperErr(w, err)
		return
	}
	s.store.LogActivity(sess.Username, "nfs_delete", "hapus export",
		map[string]any{"path": path}, clientIP(r))
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ---- fail2ban ----

func (s *Server) handleFail2banList(w http.ResponseWriter, r *http.Request) {
	if !requireSudo(w, r) {
		return
	}
	var out []helperproto.Fail2banJail
	if err := s.helper.Call(helperproto.CmdFail2banList, sessionFrom(r).Username, nil, &out); err != nil {
		writeHelperErr(w, err)
		return
	}
	if out == nil {
		out = []helperproto.Fail2banJail{}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleFail2banSave(w http.ResponseWriter, r *http.Request) {
	if !requireSudo(w, r) {
		return
	}
	sess := sessionFrom(r)
	var j helperproto.Fail2banJail
	if err := decodeBody(r, &j); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.helper.Call(helperproto.CmdFail2banSave, sess.Username, j, nil); err != nil {
		writeHelperErr(w, err)
		return
	}
	s.store.LogActivity(sess.Username, "fail2ban_save", "simpan jail",
		map[string]any{"jail": j.Name, "enabled": j.Enabled}, clientIP(r))
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleFail2banDelete(w http.ResponseWriter, r *http.Request) {
	if !requireSudo(w, r) {
		return
	}
	sess := sessionFrom(r)
	nama := chi.URLParam(r, "jail")
	if err := s.helper.Call(helperproto.CmdFail2banDelete, sess.Username,
		helperproto.PathArgs{Path: nama}, nil); err != nil {
		writeHelperErr(w, err)
		return
	}
	s.store.LogActivity(sess.Username, "fail2ban_delete", "hapus jail",
		map[string]any{"jail": nama}, clientIP(r))
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleFail2banUnban(w http.ResponseWriter, r *http.Request) {
	if !requireSudo(w, r) {
		return
	}
	sess := sessionFrom(r)
	args := helperproto.Fail2banUnbanArgs{
		Jail: chi.URLParam(r, "jail"),
		IP:   r.URL.Query().Get("ip"),
	}
	if err := s.helper.Call(helperproto.CmdFail2banUnban, sess.Username, args, nil); err != nil {
		writeHelperErr(w, err)
		return
	}
	s.store.LogActivity(sess.Username, "fail2ban_unban", "lepas blokir IP",
		map[string]any{"jail": args.Jail, "ip": args.IP}, clientIP(r))
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ---- Disk mentah ----

// handleDiskPrepare memformat (kalau diminta) lalu memasang satu disk kosong.
// Daftar disknya sendiri sudah ikut di snapshot metrik (unused_disks), jadi
// tidak ada endpoint list terpisah di sini.
func (s *Server) handleDiskPrepare(w http.ResponseWriter, r *http.Request) {
	if !requireSudo(w, r) {
		return
	}
	sess := sessionFrom(r)
	var args helperproto.DiskPrepareArgs
	if err := decodeBody(r, &args); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.helper.Call(helperproto.CmdDiskPrepare, sess.Username, args, nil); err != nil {
		writeHelperErr(w, err)
		return
	}
	s.store.LogActivity(sess.Username, "disk_prepare", "siapkan disk",
		map[string]any{"path": args.Path, "mountpoint": args.Mountpoint,
			"fstype": args.FSType, "format": args.Format}, clientIP(r))
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ---- Pool mergerfs ----

func (s *Server) handleMergerfsList(w http.ResponseWriter, r *http.Request) {
	if !requireSudo(w, r) {
		return
	}
	var pools []helperproto.MergerfsPool
	if err := s.helper.Call(helperproto.CmdMergerfsList, sessionFrom(r).Username, nil, &pools); err != nil {
		writeHelperErr(w, err)
		return
	}
	if pools == nil {
		pools = []helperproto.MergerfsPool{}
	}
	writeJSON(w, http.StatusOK, pools)
}

func (s *Server) handleMergerfsSave(w http.ResponseWriter, r *http.Request) {
	if !requireSudo(w, r) {
		return
	}
	sess := sessionFrom(r)
	var pool helperproto.MergerfsPool
	if err := decodeBody(r, &pool); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.helper.Call(helperproto.CmdMergerfsSave, sess.Username, pool, nil); err != nil {
		writeHelperErr(w, err)
		return
	}
	s.store.LogActivity(sess.Username, "mergerfs_save", "simpan pool",
		map[string]any{"mountpoint": pool.Mountpoint, "branches": pool.Branches}, clientIP(r))
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleMergerfsMount memasang/melepas pool tanpa mengubah /etc/fstab —
// dipakai untuk menonaktifkan pool sementara tanpa harus menghapusnya.
func (s *Server) handleMergerfsMount(w http.ResponseWriter, r *http.Request) {
	if !requireSudo(w, r) {
		return
	}
	sess := sessionFrom(r)
	var body helperproto.MergerfsMountArgs
	if err := decodeBody(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.helper.Call(helperproto.CmdMergerfsMount, sess.Username, body, nil); err != nil {
		writeHelperErr(w, err)
		return
	}
	aksi := "mount pool"
	if body.Lepas {
		aksi = "unmount pool"
	}
	s.store.LogActivity(sess.Username, "mergerfs_mount", aksi,
		map[string]any{"mountpoint": body.Mountpoint, "lepas": body.Lepas}, clientIP(r))
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleMergerfsDelete(w http.ResponseWriter, r *http.Request) {
	if !requireSudo(w, r) {
		return
	}
	sess := sessionFrom(r)
	mount := r.URL.Query().Get("mountpoint")
	if err := s.helper.Call(helperproto.CmdMergerfsDelete, sess.Username,
		helperproto.PathArgs{Path: mount}, nil); err != nil {
		writeHelperErr(w, err)
		return
	}
	s.store.LogActivity(sess.Username, "mergerfs_delete", "hapus pool",
		map[string]any{"mountpoint": mount}, clientIP(r))
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ---- Logs ----

func (s *Server) handleLogFileOps(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r)
	// Non-sudoer hanya melihat riwayat operasi filenya sendiri.
	username := ""
	if !sess.Sudo {
		username = sess.Username
	} else if q := r.URL.Query().Get("username"); q != "" {
		username = q
	}
	list, err := s.store.FileOps(username, queryInt(r, "limit", 100), queryInt(r, "offset", 0))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleLogActivity(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r)
	username := ""
	if !sess.Sudo {
		username = sess.Username
	} else if q := r.URL.Query().Get("username"); q != "" {
		username = q
	}
	// Halaman Activity Logs fokus ke event login/logout; aksi admin lain
	// tersimpan di tabel yang sama dan bisa diminta lewat ?scope=all.
	types := store.LoginEventTypes
	if r.URL.Query().Get("scope") == "all" {
		types = nil
	}
	list, err := s.store.Activity(types, username, queryInt(r, "limit", 100), queryInt(r, "offset", 0))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// ---- Components ----

// handleComponents mengambil seluruh katalog dalam SATU panggilan helper.
//
// Sebelumnya daftar nama komponen ditulis ulang di sini dan dipanggil satu per
// satu: katalog helper yang bertambah tidak pernah sampai ke UI (halaman cuma
// menampilkan lima nama yang kebetulan ditulis di sini), dan tiap komponen
// berarti satu round-trip socket plus probe versi berurutan.
func (s *Server) handleComponents(w http.ResponseWriter, r *http.Request) {
	var out []helperproto.ComponentStatus
	// ?fresh=1 datang dari tombol Refresh manual; pemuatan halaman biasa
	// tetap boleh dilayani dari cache helper.
	fresh := r.URL.Query().Get("fresh") == "1"
	if err := s.helper.Call(helperproto.CmdComponentStatusAll, sessionFrom(r).Username,
		helperproto.ComponentArgs{Name: "all", Fresh: fresh}, &out); err != nil {
		writeHelperErr(w, err)
		return
	}
	if out == nil {
		out = []helperproto.ComponentStatus{}
	}
	writeJSON(w, http.StatusOK, out)
}

// handleComponentProgress dibaca berkala oleh halaman Components selama
// menunggu instalasi yang sedang berjalan. Perintah installnya sendiri tetap
// sinkron — endpoint ini hanya jendela ke kemajuannya, jadi tidak ada kontrak
// lama yang berubah.
func (s *Server) handleComponentProgress(w http.ResponseWriter, r *http.Request) {
	if !requireSudo(w, r) {
		return
	}
	var out helperproto.ComponentProgress
	if err := s.helper.Call(helperproto.CmdComponentProgress, sessionFrom(r).Username, nil, &out); err != nil {
		writeHelperErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleComponentInstall(w http.ResponseWriter, r *http.Request) {
	s.componentAction(w, r, helperproto.CmdComponentInstall, "install", false)
}

func (s *Server) handleComponentUninstall(w http.ResponseWriter, r *http.Request) {
	// ?purge=1 meminta data komponen ikut dihapus. Default-nya tidak: data
	// yang hilang tidak bisa dikembalikan, jadi harus diminta eksplisit dari
	// UI, bukan jadi efek samping tombol Hapus.
	s.componentAction(w, r, helperproto.CmdComponentUninstall, "uninstall",
		r.URL.Query().Get("purge") == "1")
}

func (s *Server) componentAction(w http.ResponseWriter, r *http.Request, cmd, label string, purge bool) {
	// Instalasi/penghapusan paket ke sistem = operasi berisiko tinggi.
	if !requireSudo(w, r) {
		return
	}
	sess := sessionFrom(r)
	name := chi.URLParam(r, "name")
	var st helperproto.ComponentStatus
	args := helperproto.ComponentArgs{Name: name, Purge: purge}
	if err := s.helper.Call(cmd, sess.Username, args, &st); err != nil {
		writeHelperErr(w, err)
		return
	}
	s.store.LogActivity(sess.Username, "component_"+label, label,
		map[string]any{"component": name, "purge": args.Purge}, clientIP(r))
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) handleComponentService(w http.ResponseWriter, r *http.Request) {
	if !requireSudo(w, r) {
		return
	}
	sess := sessionFrom(r)
	name, action := chi.URLParam(r, "name"), chi.URLParam(r, "action")
	if err := s.helper.Call(helperproto.CmdComponentService, sess.Username,
		helperproto.ComponentArgs{Name: name, Action: action}, nil); err != nil {
		writeHelperErr(w, err)
		return
	}
	s.store.LogActivity(sess.Username, "component_service", action,
		map[string]any{"component": name}, clientIP(r))
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
