package api

import (
	"archive/zip"
	"encoding/json"
	"io"
	"log"
	"mime"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"linux-dashboard/OxidiLily/internal/helperclient"
	"linux-dashboard/OxidiLily/internal/helperproto"
)

// Semua operasi file lewat helper daemon, bukan langsung dari web app:
// web app berjalan sebagai satu system user tetap, sedangkan file di server
// dimiliki masing-masing akun Linux. Helper yang men-drop privilege ke user
// yang login, sehingga kernel tetap jadi penegak izin.

type fileRoot struct {
	Name string `json:"name"`
	Path string `json:"path"`
	// Pool menandai pintasan pool mergerfs. UI mengelompokkannya di belakang
	// satu label "Disk pool :" supaya nama pool berdiri sendiri sebagai tombol,
	// bukan ikut tertulis di dalam setiap tombolnya.
	Pool bool `json:"pool,omitempty"`
}

// dataDirs adalah folder data milik user sendiri: ~/DATA/AppData, ~/DATA/Media,
// dan seterusnya. Semuanya di dalam home, jadi tidak butuh pengecualian jail
// apa pun di helper — kernel dan izin Unix yang tetap jadi penegak akhir.
func dataDirs(home string) []string {
	names := []string{"AppData", "Documents", "Downloads", "Gallery", "Media"}
	out := make([]string, 0, len(names))
	for _, n := range names {
		out = append(out, filepath.Join(home, "DATA", n))
	}
	return out
}

// siapkanDataDirs membuat ~/DATA/* yang belum ada. Dipanggil dari sini, bukan
// dari login: sesi berumur sampai 12 jam, jadi user yang sudah login sebelum
// folder ini diperkenalkan (atau yang foldernya terhapus) tidak akan pernah
// melewati login lagi — dan daftar root di bawah menampilkan folder yang tidak
// ada. Tempat yang mengiklankan folder ini juga yang memastikannya ada.
//
// mkdir dijalankan lewat helper supaya prosesnya berjalan sebagai user itu
// sendiri: folder yang dibuat root akan jadi milik root di dalam home orang.
// `MkdirAll` idempoten — folder yang sudah ada tidak diubah izinnya.
func (s *Server) siapkanDataDirs(username, home string, dirs []string) {
	if home == "" || home == "/" {
		return
	}
	for _, d := range dirs {
		if err := s.helper.Call(helperproto.CmdFileMkdir, username,
			helperproto.PathArgs{Path: d}, nil); err != nil {
			log.Printf("membuat %s untuk %s gagal: %v", d, username, err)
		}
	}
}

// "Root (/)" tetap sudo-only: membukanya untuk semua akun sama saja
// membatalkan jail home.
func (s *Server) handleFileRoots(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r)
	dirs := dataDirs(sess.Home)
	s.siapkanDataDirs(sess.Username, sess.Home, dirs)
	roots := []fileRoot{{Name: "Home", Path: sess.Home}}
	for _, d := range dirs {
		roots = append(roots, fileRoot{Name: filepath.Base(d), Path: d})
	}
	if sess.Sudo {
		roots = append(roots, fileRoot{Name: "Root (/)", Path: "/"})
		roots = append(roots, poolRoots(s, sess.Username)...)
	}
	writeJSON(w, http.StatusOK, roots)
}

// poolRoots menambahkan pintasan ke tiap pool mergerfs, tepat setelah
// "Root (/)". Hanya untuk sudoer: mount point pool ada di luar home, jadi user
// biasa akan ditolak saat membukanya — pintasan yang pasti gagal lebih buruk
// daripada tidak ada pintasan.
//
// Gagal membaca daftar pool sengaja tidak menggagalkan permintaan: mergerfs
// bisa saja belum terpasang, dan file manager tetap harus bisa dipakai tanpa
// pintasan ini.
func poolRoots(s *Server, username string) []fileRoot {
	var pools []helperproto.MergerfsPool
	if err := s.helper.Call(helperproto.CmdMergerfsList, username, nil, &pools); err != nil {
		return nil
	}
	out := make([]fileRoot, 0, len(pools))
	for _, p := range pools {
		// Pool yang sedang dilepas tidak dibuatkan pintasan: mount point-nya
		// dibuang saat pool dilepas, jadi tombolnya akan menunjuk folder yang
		// tidak ada. Bahkan kalau foldernya masih tersisa, isinya bukan isi
		// pool — pintasan yang membawa user ke tempat yang salah lebih buruk
		// daripada pintasan yang hilang selama pool tidak aktif.
		if !p.Mounted {
			continue
		}
		out = append(out, fileRoot{
			Name: hurufBesarAwal(filepath.Base(p.Mountpoint)),
			Path: p.Mountpoint,
			Pool: true,
		})
	}
	return out
}

// hurufBesarAwal membesarkan huruf pertama untuk label yang dibaca user.
// Path aslinya tidak ikut berubah — /mnt/pool tetap /mnt/pool, yang berbeda
// hanya tulisannya di layar.
func hurufBesarAwal(s string) string {
	r := []rune(s)
	if len(r) == 0 {
		return s
	}
	return strings.ToUpper(string(r[0])) + string(r[1:])
}

func (s *Server) handleFileList(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r)
	path := r.URL.Query().Get("path")
	if path == "" {
		path = sess.Home
	}
	var entries []helperproto.FileEntry
	if err := s.helper.Call(helperproto.CmdFileList, sess.Username,
		helperproto.PathArgs{Path: path}, &entries); err != nil {
		writeHelperErr(w, err)
		return
	}
	if entries == nil {
		entries = []helperproto.FileEntry{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"path": filepath.Clean(path), "entries": entries})
}

type pathBody struct {
	Path string `json:"path"`
}

type twoPathBody struct {
	Source string `json:"source"`
	Dest   string `json:"dest"`
}

func (s *Server) handleMkdir(w http.ResponseWriter, r *http.Request) {
	s.simpleFileOp(w, r, helperproto.CmdFileMkdir, "mkdir")
}

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	s.simpleFileOp(w, r, helperproto.CmdFileRemove, "delete")
}

func (s *Server) simpleFileOp(w http.ResponseWriter, r *http.Request, cmd, opName string) {
	sess := sessionFrom(r)
	var body pathBody
	if err := decodeBody(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.helper.Call(cmd, sess.Username, helperproto.PathArgs{Path: body.Path}, nil); err != nil {
		writeHelperErr(w, err)
		return
	}
	s.usage.buang(sess.Username)
	s.store.LogFileOp(sess.Username, opName, body.Path, "")
	s.store.LogActivity(sess.Username, "file_"+opName, opName, map[string]any{"path": body.Path}, clientIP(r))
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleRename(w http.ResponseWriter, r *http.Request) {
	s.twoPathOp(w, r, helperproto.CmdFileRename, "rename")
}

func (s *Server) handleCopy(w http.ResponseWriter, r *http.Request) {
	s.twoPathOp(w, r, helperproto.CmdFileCopy, "copy")
}

func (s *Server) handleMove(w http.ResponseWriter, r *http.Request) {
	s.twoPathOp(w, r, helperproto.CmdFileMove, "move")
}

func (s *Server) twoPathOp(w http.ResponseWriter, r *http.Request, cmd, opName string) {
	sess := sessionFrom(r)
	var body twoPathBody
	if err := decodeBody(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.helper.Call(cmd, sess.Username,
		helperproto.TwoPathArgs{Source: body.Source, Dest: body.Dest}, nil); err != nil {
		writeHelperErr(w, err)
		return
	}
	s.usage.buang(sess.Username)
	s.store.LogFileOp(sess.Username, opName, body.Source, body.Dest)
	s.store.LogActivity(sess.Username, "file_"+opName, opName,
		map[string]any{"source": body.Source, "dest": body.Dest}, clientIP(r))
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type permissionBody struct {
	Path      string `json:"path"`
	Mode      string `json:"mode"` // string oktal, mis. "755"
	Owner     string `json:"owner"`
	Group     string `json:"group"`
	Recursive bool   `json:"recursive"`
}

func (s *Server) handlePermissions(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r)
	var body permissionBody
	if err := decodeBody(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if body.Mode != "" {
		mode, err := parseOctalMode(body.Mode)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := s.helper.Call(helperproto.CmdFileChmod, sess.Username,
			helperproto.ChmodArgs{Path: body.Path, Mode: mode}, nil); err != nil {
			writeHelperErr(w, err)
			return
		}
	}
	if body.Owner != "" || body.Group != "" {
		if err := s.helper.Call(helperproto.CmdFileChown, sess.Username,
			helperproto.ChownArgs{Path: body.Path, Owner: body.Owner, Group: body.Group, Recursive: body.Recursive}, nil); err != nil {
			writeHelperErr(w, err)
			return
		}
	}
	s.store.LogActivity(sess.Username, "file_permissions", "chmod/chown",
		map[string]any{"path": body.Path, "mode": body.Mode, "owner": body.Owner, "group": body.Group}, clientIP(r))
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func parseOctalMode(s string) (uint32, error) {
	s = strings.TrimPrefix(s, "0o")
	s = strings.TrimPrefix(s, "0")
	if s == "" {
		s = "0"
	}
	var mode uint32
	for _, c := range s {
		if c < '0' || c > '7' {
			return 0, errInvalidMode
		}
		mode = mode*8 + uint32(c-'0')
	}
	if mode > 0o7777 {
		return 0, errInvalidMode
	}
	return mode, nil
}

var errInvalidMode = &jsonError{"mode permission harus oktal, mis. 755"}

type jsonError struct{ msg string }

func (e *jsonError) Error() string { return e.msg }

// handleUpload menerima multipart secara streaming: tiap part disalin langsung
// ke helper → disk. Tidak ada batas ukuran, dan tidak ada file yang ditahan
// penuh di RAM (ParseMultipartForm sengaja tidak dipakai).
func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r)
	dir := r.URL.Query().Get("path")
	if dir == "" {
		dir = sess.Home
	}
	mr, err := r.MultipartReader()
	if err != nil {
		writeErr(w, http.StatusBadRequest, "request bukan multipart: "+err.Error())
		return
	}
	var saved []string
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			writeErr(w, http.StatusBadRequest, "gagal membaca part: "+err.Error())
			return
		}
		rel := uploadRelPath(partFileName(part))
		if rel == "" {
			part.Close()
			continue
		}
		dest := filepath.Join(dir, rel)
		// Upload folder mengirim path relatif, jadi direktori antaranya harus
		// ada lebih dulu. Dibuat lewat helper supaya kepemilikannya tetap milik
		// user yang login, bukan user service web.
		if sub := filepath.Dir(rel); sub != "." {
			if err := s.helper.Call(helperproto.CmdFileMkdir, sess.Username,
				helperproto.PathArgs{Path: filepath.Dir(dest)}, nil); err != nil {
				part.Close()
				writeHelperErr(w, err)
				return
			}
		}
		stream, err := s.helper.Stream(helperproto.CmdFileWrite, sess.Username,
			helperproto.WriteArgs{Path: dest})
		if err != nil {
			part.Close()
			writeHelperErr(w, err)
			return
		}
		_, copyErr := io.Copy(stream, part)
		part.Close()
		// Selesai memberi EOF ke helper lalu menunggu konfirmasi bahwa berkasnya
		// benar-benar tertulis; tanpa menunggu, upload yang ditolak izin tetap
		// dilaporkan berhasil.
		doneErr := stream.Selesai()
		stream.Close()
		if copyErr != nil {
			writeErr(w, http.StatusInternalServerError, "upload gagal: "+copyErr.Error())
			return
		}
		if doneErr != nil {
			writeHelperErr(w, doneErr)
			return
		}
		s.store.LogFileOp(sess.Username, "upload", "", dest)
		saved = append(saved, dest)
	}
	s.usage.buang(sess.Username)
	s.store.LogActivity(sess.Username, "file_upload", "upload",
		map[string]any{"files": saved, "dir": dir}, clientIP(r))
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "files": saved})
}

// partFileName mengambil nama berkas MENTAH dari header Content-Disposition.
// multipart.Part.FileName() menjalankan filepath.Base sejak Go 1.17, jadi
// "folder/sub/berkas.txt" dari webkitRelativePath keburu terpotong jadi
// "berkas.txt" — struktur folder yang diunggah user hilang dan berkas
// dengan nama sama saling menimpa.
func partFileName(p *multipart.Part) string {
	_, params, err := mime.ParseMediaType(p.Header.Get("Content-Disposition"))
	if err == nil && params["filename"] != "" {
		return params["filename"]
	}
	return p.FileName()
}

// uploadRelPath membersihkan nama part multipart jadi path relatif yang aman.
// Browser mengirim "folder/sub/berkas.txt" saat user mengunggah satu folder
// (webkitRelativePath), jadi nama part TIDAK bisa diperlakukan sebagai nama
// berkas polos. Clean di atas "/" menetralkan ".." dan path absolut, sehingga
// tujuan tidak pernah keluar dari direktori yang sedang dibuka. Mengembalikan
// string kosong kalau tidak ada nama yang tersisa.
func uploadRelPath(partName string) string {
	rel := strings.TrimPrefix(filepath.Clean("/"+partName), "/")
	if rel == "." {
		return ""
	}
	return rel
}

// handleArchive menstream beberapa path (file dan/atau folder) sebagai satu
// zip. Tidak ada command helper baru: isi zip dirakit di sini dari file.list +
// file.read, jadi helper tetap satu-satunya yang menyentuh disk sebagai user
// yang login — kernel yang tetap menegakkan izin per berkas.
func (s *Server) handleArchive(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r)
	paths := r.URL.Query()["path"]
	if len(paths) == 0 {
		writeErr(w, http.StatusBadRequest, "parameter path wajib diisi")
		return
	}
	name := "files.zip"
	if len(paths) == 1 {
		name = filepath.Base(paths[0]) + ".zip"
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", "attachment; filename*=UTF-8''"+urlEscape(name))
	src := zipSumber{
		list: func(path string) ([]helperproto.FileEntry, error) {
			var entries []helperproto.FileEntry
			err := s.helper.Call(helperproto.CmdFileList, sess.Username,
				helperproto.PathArgs{Path: path}, &entries)
			return entries, err
		},
		read: func(path string) (io.ReadCloser, error) {
			return s.helper.Stream(helperproto.CmdFileRead, sess.Username,
				helperproto.PathArgs{Path: path})
		},
	}
	zw := zip.NewWriter(w)
	for _, p := range paths {
		zipPath(zw, src, p, filepath.Base(p))
	}
	// Header sudah terkirim, jadi kegagalan di tengah tidak bisa lagi jadi
	// status HTTP: zip yang tidak sempat ditutup akan terbaca rusak di sisi
	// klien, dan itu memang satu-satunya sinyal yang tersisa.
	if err := zw.Close(); err != nil {
		return
	}
	s.store.LogFileOp(sess.Username, "download", strings.Join(paths, ", "), "")
	s.store.LogActivity(sess.Username, "file_download", "download zip",
		map[string]any{"paths": paths}, clientIP(r))
}

// zipSumber memisahkan penyusunan zip dari helper daemon: dua operasi yang
// dibutuhkan hanya "daftar isi folder" dan "buka berkas untuk dibaca".
type zipSumber struct {
	list func(path string) ([]helperproto.FileEntry, error)
	read func(path string) (io.ReadCloser, error)
}

// zipPath membungkus satu path pilihan user. Jenisnya belum diketahui, jadi
// diprobe sekali lewat list: berhasil = direktori, gagal = berkas biasa.
func zipPath(zw *zip.Writer, src zipSumber, path, rel string) {
	if entries, err := src.list(path); err == nil {
		zipDir(zw, src, rel, entries)
		return
	}
	zipFile(zw, src, path, rel)
}

// zipDir menulis isi satu direktori yang SUDAH terdaftar. Jenis tiap entri
// dibaca dari hasil list induknya, bukan diprobe ulang satu per satu: tiap
// panggilan helper berarti satu fork+exec worker sebagai user yang login,
// jadi probe per berkas melipatgandakan biaya unduhan folder besar.
//
// Helper memakai Lstat, sehingga symlink ke folder terhitung berkas biasa dan
// rekursi ini tidak bisa berputar.
func zipDir(zw *zip.Writer, src zipSumber, rel string, entries []helperproto.FileEntry) {
	if len(entries) == 0 {
		// Folder kosong tetap muncul di zip; tanpa entri ini strukturnya
		// hilang begitu diekstrak.
		_, _ = zw.Create(rel + "/")
		return
	}
	for _, e := range entries {
		if !e.IsDir {
			zipFile(zw, src, e.Path, rel+"/"+e.Name)
			continue
		}
		sub, err := src.list(e.Path)
		if err != nil {
			continue
		}
		zipDir(zw, src, rel+"/"+e.Name, sub)
	}
}

// zipFile menyalin satu berkas ke dalam zip. Entri yang gagal dibaca dilewati
// — satu berkas tanpa izin tidak boleh membatalkan seluruh unduhan.
func zipFile(zw *zip.Writer, src zipSumber, path, rel string) {
	rc, err := src.read(path)
	if err != nil {
		return
	}
	defer rc.Close()
	// Store, bukan Deflate: isi folder di file manager didominasi media dan
	// arsip yang sudah terkompresi, jadi deflate hanya membakar CPU dan
	// memperlambat unduhan tanpa mengecilkan hasilnya.
	f, err := zw.CreateHeader(&zip.FileHeader{Name: rel, Method: zip.Store})
	if err != nil {
		return
	}
	_, _ = io.Copy(f, rc)
}

// usageTTL menentukan kapan hasil dianggap perlu dihitung ulang. Ukuran folder
// mahal dihitung — bukan karena byte-nya, tapi karena tiap inode harus di-stat.
const usageTTL = 5 * time.Minute

// usageCache menyimpan hasil per (user, path). Dikunci ke user karena dua akun
// melihat isi direktori yang sama secara berbeda: yang tidak bisa dibaca
// dilewati, jadi totalnya pun berbeda.
//
// Hasil basi tetap dilayani lalu diperbarui di latar (stale-while-revalidate):
// menahan user beberapa detik demi angka yang biasanya sama saja bukan
// pertukaran yang bagus, sedangkan operasi tulis apa pun langsung membuang
// cache-nya, jadi angka basi tidak akan bertahan setelah ada perubahan nyata.
type usageCache struct {
	mu    sync.Mutex
	m     map[string]usageEntri
	jalan map[string]bool // perhitungan latar yang sedang berjalan
}

type usageEntri struct {
	hasil helperproto.UsageHasil
	waktu time.Time
}

func newUsageCache() *usageCache {
	return &usageCache{m: map[string]usageEntri{}, jalan: map[string]bool{}}
}

func kunciUsage(username, path string) string { return username + "\x00" + path }

func (c *usageCache) ambil(username, path string, sekarang time.Time) (hasil helperproto.UsageHasil, segar, ada bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[kunciUsage(username, path)]
	if !ok {
		return helperproto.UsageHasil{}, false, false
	}
	return e.hasil, sekarang.Sub(e.waktu) <= usageTTL, true
}

func (c *usageCache) simpan(username, path string, hasil helperproto.UsageHasil, sekarang time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m[kunciUsage(username, path)] = usageEntri{hasil: hasil, waktu: sekarang}
}

// mulaiLatar menandai satu path sedang dihitung. Mengembalikan false kalau
// sudah ada perhitungan yang berjalan — tanpa ini, membuka folder yang sama
// berulang kali melahirkan satu penelusuran penuh per permintaan.
func (c *usageCache) mulaiLatar(username, path string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	k := kunciUsage(username, path)
	if c.jalan[k] {
		return false
	}
	c.jalan[k] = true
	return true
}

func (c *usageCache) selesaiLatar(username, path string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.jalan, kunciUsage(username, path))
}

// buang mengosongkan cache milik satu user. Dipanggil setiap kali user itu
// mengubah berkas: ukuran yang basi setelah menghapus folder lebih buruk
// daripada menunggu penelusuran ulang.
func (c *usageCache) buang(username string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for k := range c.m {
		if strings.HasPrefix(k, username+"\x00") {
			delete(c.m, k)
		}
	}
}

// handleUsage menghitung ukuran isi satu direktori — setara `du -xb`.
// Penelusurannya dikerjakan satu worker helper sekaligus (file.usage), bukan
// list per direktori dari sini: satu panggilan helper = satu fork+exec, dan
// folder dengan puluhan ribu subdirektori akan menghabiskan menit hanya untuk
// membuat proses.
func (s *Server) handleUsage(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r)
	path := r.URL.Query().Get("path")
	if path == "" {
		writeErr(w, http.StatusBadRequest, "parameter path wajib diisi")
		return
	}
	hasil, segar, ada := s.usage.ambil(sess.Username, path, time.Now())
	if ada {
		if !segar {
			s.perbaruiUsage(sess.Username, path)
		}
	} else {
		var err error
		hasil, err = s.hitungUsage(sess.Username, path)
		if err != nil {
			writeHelperErr(w, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"path":    path,
		"size":    hasil.Size,
		"files":   hasil.Files,
		"dirs":    hasil.Dirs,
		"partial": hasil.Partial,
	})
}

func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request) {
	s.streamFile(w, r, true)
}

// maxEditBytes membatasi file yang boleh dibuka di editor teks. Membuka file
// biner besar di textarea browser hanya akan menggantung tab.
const maxEditBytes = 1 << 20

// handleFileContent membaca isi file sebagai teks untuk editor. Berbeda dari
// preview: hasilnya JSON (bukan stream biner) dan dibatasi ukurannya.
func (s *Server) handleFileContent(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r)
	path := r.URL.Query().Get("path")
	if path == "" {
		writeErr(w, http.StatusBadRequest, "parameter path wajib diisi")
		return
	}
	stream, err := s.helper.Stream(helperproto.CmdFileRead, sess.Username,
		helperproto.PathArgs{Path: path})
	if err != nil {
		writeHelperErr(w, err)
		return
	}
	defer stream.Close()
	isi, err := io.ReadAll(io.LimitReader(stream, maxEditBytes+1))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if len(isi) > maxEditBytes {
		writeErr(w, http.StatusRequestEntityTooLarge,
			"File lebih dari 1 MB — terlalu besar untuk diedit di browser")
		return
	}
	// File biner tidak boleh masuk textarea: menyimpannya kembali akan merusak
	// isinya karena byte non-UTF8 diganti karakter pengganti.
	if !utf8.Valid(isi) {
		writeErr(w, http.StatusUnsupportedMediaType, "File ini bukan teks — tidak bisa diedit")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"path": path, "content": string(isi)})
}

type fileContentBody struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// handleFileSave menulis isi editor kembali ke file. Dipakai juga untuk
// membuat file baru (isi kosong).
func (s *Server) handleFileSave(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r)
	var body fileContentBody
	if err := decodeBody(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if body.Path == "" || !strings.HasPrefix(body.Path, "/") {
		writeErr(w, http.StatusBadRequest, "path harus absolut")
		return
	}
	if len(body.Content) > maxEditBytes {
		writeErr(w, http.StatusRequestEntityTooLarge, "Isi melebihi 1 MB")
		return
	}
	stream, err := s.helper.Stream(helperproto.CmdFileWrite, sess.Username,
		helperproto.WriteArgs{Path: body.Path})
	if err != nil {
		writeHelperErr(w, err)
		return
	}
	_, werr := io.WriteString(stream, body.Content)
	cerr := stream.Selesai()
	stream.Close()
	if werr != nil {
		writeErr(w, http.StatusInternalServerError, "gagal menulis file")
		return
	}
	if cerr != nil {
		writeHelperErr(w, cerr)
		return
	}
	s.usage.buang(sess.Username)
	s.store.LogFileOp(sess.Username, "write", body.Path, "")
	s.store.LogActivity(sess.Username, "file_write", "tulis file",
		map[string]any{"path": body.Path, "bytes": len(body.Content)}, clientIP(r))
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "path": body.Path})
}

func (s *Server) handlePreview(w http.ResponseWriter, r *http.Request) {
	s.streamFile(w, r, false)
}

// tipeMediaTambahan menambal ekstensi yang tidak selalu ada di tabel MIME
// sistem. mime.TypeByExtension membaca /etc/mime.types, dan di instalasi
// Ubuntu minimal (container, image cloud) berkas itu tipis atau tidak ada
// sama sekali — .mkv dan .m4v hampir selalu absen. Tanpa tipe yang benar
// jawabannya jadi application/octet-stream, dan X-Content-Type-Options:
// nosniff melarang browser menebak sendiri, jadi <video> menolak memutarnya
// bahkan untuk berkas yang codec-nya sebenarnya didukung.
var tipeMediaTambahan = map[string]string{
	".mp4":  "video/mp4",
	".m4v":  "video/mp4",
	".mkv":  "video/x-matroska",
	".webm": "video/webm",
	".ogv":  "video/ogg",
	".mov":  "video/quicktime",
	".avi":  "video/x-msvideo",
	".mpeg": "video/mpeg",
	".mpg":  "video/mpeg",
	".ts":   "video/mp2t",
	".mp3":  "audio/mpeg",
	".m4a":  "audio/mp4",
	".aac":  "audio/aac",
	".ogg":  "audio/ogg",
	".opus": "audio/ogg",
	".flac": "audio/flac",
	".wav":  "audio/wav",
}

func tipeKonten(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	// Tabel sendiri didahulukan: /etc/mime.types di sebagian distro memetakan
	// .ts ke text/vnd.trolltech.linguist, yang membuat berkas video MPEG-TS
	// dikirim sebagai teks.
	if t, ok := tipeMediaTambahan[ext]; ok {
		return t
	}
	if t := mime.TypeByExtension(ext); t != "" {
		return t
	}
	return "application/octet-stream"
}

// rentangDiminta membaca header Range untuk satu rentang byte. Hanya bentuk
// "bytes=awal-akhir", "bytes=awal-", dan "bytes=-n" yang dilayani; permintaan
// multi-rentang (dipisah koma) dijawab sebagai berkas penuh, yang sah menurut
// RFC 7233 dan tidak pernah dikirim pemutar media arus utama.
//
// Nilai balik: awal, akhir (-1 = sampai ujung), suffix (true untuk "bytes=-n",
// yang butuh ukuran berkas lebih dulu), dan ok.
func rentangDiminta(h string) (awal, akhir int64, suffix, ok bool) {
	if !strings.HasPrefix(h, "bytes=") {
		return 0, 0, false, false
	}
	spec := strings.TrimPrefix(h, "bytes=")
	if strings.Contains(spec, ",") {
		return 0, 0, false, false
	}
	kiri, kanan, found := strings.Cut(spec, "-")
	if !found {
		return 0, 0, false, false
	}
	kiri, kanan = strings.TrimSpace(kiri), strings.TrimSpace(kanan)
	if kiri == "" {
		n, err := strconv.ParseInt(kanan, 10, 64)
		if err != nil || n <= 0 {
			return 0, 0, false, false
		}
		return n, -1, true, true // n = jumlah byte terakhir yang diminta
	}
	a, err := strconv.ParseInt(kiri, 10, 64)
	if err != nil || a < 0 {
		return 0, 0, false, false
	}
	if kanan == "" {
		return a, -1, false, true
	}
	b, err := strconv.ParseInt(kanan, 10, 64)
	if err != nil || b < a {
		return 0, 0, false, false
	}
	return a, b, false, true
}

func (s *Server) streamFile(w http.ResponseWriter, r *http.Request, asAttachment bool) {
	sess := sessionFrom(r)
	path := r.URL.Query().Get("path")
	if path == "" {
		writeErr(w, http.StatusBadRequest, "parameter path wajib diisi")
		return
	}

	buka := func(offset, length int64) (*helperclient.Stream, helperproto.FileEntry, error) {
		st, err := s.helper.Stream(helperproto.CmdFileRead, sess.Username,
			helperproto.ReadArgs{Path: path, Offset: offset, Length: length})
		if err != nil {
			return nil, helperproto.FileEntry{}, err
		}
		var meta helperproto.FileEntry
		if st.Resp != nil && len(st.Resp.Data) > 0 {
			_ = json.Unmarshal(st.Resp.Data, &meta)
		}
		return st, meta, nil
	}

	awal, akhir, suffix, adaRentang := int64(0), int64(-1), false, false
	if h := r.Header.Get("Range"); h != "" {
		awal, akhir, suffix, adaRentang = rentangDiminta(h)
	}

	stream, meta, err := buka(0, 0)
	if err != nil {
		writeHelperErr(w, err)
		return
	}
	defer func() { stream.Close() }()

	// "bytes=-n" baru bisa diterjemahkan jadi offset setelah ukuran berkas
	// diketahui, dan ukuran itu datang bersama response awal helper. Stream
	// pertama dibuang lalu dibuka ulang dari offset yang benar — satu
	// perjalanan tambahan untuk bentuk Range yang praktis tidak pernah
	// dikirim pemutar media, bukan beban di jalur normal.
	if adaRentang && suffix {
		n := awal
		if n > meta.Size {
			n = meta.Size
		}
		awal, akhir = meta.Size-n, meta.Size-1
	}

	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Content-Type", tipeKonten(path))
	if asAttachment {
		w.Header().Set("Content-Disposition", "attachment; filename*=UTF-8''"+urlEscape(filepath.Base(path)))
	} else {
		// Preview di-render inline; header ini mencegah browser mengeksekusi
		// HTML/SVG milik user sebagai halaman dashboard.
		w.Header().Set("Content-Disposition", "inline")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "sandbox")
	}

	if !adaRentang {
		if meta.Size > 0 {
			w.Header().Set("Content-Length", itoa(meta.Size))
		}
		_, _ = io.Copy(w, stream)
		if asAttachment {
			s.store.LogFileOp(sess.Username, "download", path, "")
		}
		return
	}

	if awal >= meta.Size {
		// 416 wajib membawa Content-Range supaya klien tahu ukuran sebenarnya
		// dan bisa meminta ulang, bukan menyerah.
		w.Header().Set("Content-Range", "bytes */"+itoa(meta.Size))
		w.Header().Del("Content-Length")
		w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
		return
	}
	if akhir < 0 || akhir >= meta.Size {
		akhir = meta.Size - 1
	}
	panjang := akhir - awal + 1

	// Stream pembuka dipakai apa adanya hanya kalau rentangnya kebetulan
	// seluruh berkas; selain itu dibuka ulang dengan offset yang diminta.
	if awal != 0 || panjang != meta.Size {
		stream.Close()
		stream, _, err = buka(awal, panjang)
		if err != nil {
			writeHelperErr(w, err)
			return
		}
	}

	w.Header().Set("Content-Range", "bytes "+itoa(awal)+"-"+itoa(akhir)+"/"+itoa(meta.Size))
	w.Header().Set("Content-Length", itoa(panjang))
	w.WriteHeader(http.StatusPartialContent)
	_, _ = io.CopyN(w, stream, panjang)
	if asAttachment {
		s.store.LogFileOp(sess.Username, "download", path, "")
	}
}

func (s *Server) hitungUsage(username, path string) (helperproto.UsageHasil, error) {
	var hasil helperproto.UsageHasil
	if err := s.helper.Call(helperproto.CmdFileUsage, username,
		helperproto.PathArgs{Path: path}, &hasil); err != nil {
		return hasil, err
	}
	s.usage.simpan(username, path, hasil, time.Now())
	return hasil, nil
}

// perbaruiUsage menghitung ulang di latar. Permintaan yang memicunya sudah
// dijawab dengan angka lama, jadi kegagalan di sini cukup meninggalkan entri
// lama — percobaan berikutnya akan mencoba lagi.
func (s *Server) perbaruiUsage(username, path string) {
	if !s.usage.mulaiLatar(username, path) {
		return
	}
	go func() {
		defer s.usage.selesaiLatar(username, path)
		_, _ = s.hitungUsage(username, path)
	}()
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

func urlEscape(s string) string {
	const hex = "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') ||
			c == '-' || c == '_' || c == '.' || c == '~' {
			b.WriteByte(c)
			continue
		}
		b.WriteByte('%')
		b.WriteByte(hex[c>>4])
		b.WriteByte(hex[c&0xf])
	}
	return b.String()
}
