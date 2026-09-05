package api

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"linux-dashboard/OxidiLily/internal/helperproto"
	"linux-dashboard/OxidiLily/internal/store"
)

// Seluruh menu Docker mensyaratkan sudo. Ini bukan sekadar soal UX: siapa pun
// yang bisa bicara ke docker.sock bisa menjalankan container yang mem-bind
// mount `/` host, jadi membuka menu ini untuk non-sudoer sama dengan membuka
// jalur eskalasi ke root.

type container struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Image  string `json:"image"`
	State  string `json:"state"`
	Status string `json:"status"`
	Ports  string `json:"ports"`
}

func (s *Server) dockerRun(username string, dir string, args ...string) (helperproto.ExecResult, error) {
	var res helperproto.ExecResult
	err := s.helper.Call(helperproto.CmdDockerExec, username,
		helperproto.DockerExecArgs{Args: args, Dir: dir}, &res)
	return res, err
}

func (s *Server) handleDockerContainers(w http.ResponseWriter, r *http.Request) {
	if !requireSudo(w, r) {
		return
	}
	res, err := s.dockerRun(sessionFrom(r).Username, "", "ps", "-a", "--format", "{{json .}}")
	if err != nil {
		writeHelperErr(w, err)
		return
	}
	out := []container{}
	for _, line := range strings.Split(strings.TrimSpace(res.Stdout), "\n") {
		if line == "" {
			continue
		}
		// `docker ps --format {{json .}}` mengeluarkan satu objek per baris,
		// bukan satu array — jadi di-decode baris per baris.
		var row struct {
			ID, Names, Image, State, Status, Ports string
		}
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			continue
		}
		out = append(out, container{
			ID: row.ID, Name: row.Names, Image: row.Image,
			State: row.State, Status: row.Status, Ports: row.Ports,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// ---- Aksi per container ----

// Whitelist aksi container. Nilai dari URL TIDAK pernah diteruskan langsung ke
// CLI docker: `action` yang tidak dikenal harus berhenti di sini, bukan jadi
// argumen `docker <apa pun>`.
var aksiContainer = map[string][]string{
	"start":   {"start"},
	"stop":    {"stop"},
	"restart": {"restart"},
	"remove":  {"rm", "-f"},
}

func (s *Server) handleContainerAction(w http.ResponseWriter, r *http.Request) {
	if !requireSudo(w, r) {
		return
	}
	sess := sessionFrom(r)
	id := chi.URLParam(r, "id")
	action := chi.URLParam(r, "action")
	argsAwal, ok := aksiContainer[action]
	if !ok {
		writeErr(w, http.StatusBadRequest, "aksi container tidak dikenal")
		return
	}
	if id == "" {
		writeErr(w, http.StatusBadRequest, "id container kosong")
		return
	}
	// `docker start` pada container yang sudah jalan keluar dengan status 0
	// tanpa melakukan apa pun — tanpa cek ini UI melaporkan "berhasil" untuk
	// aksi yang sebenarnya tidak terjadi.
	if action == "start" && s.containerBerjalan(sess.Username, id) {
		writeErr(w, http.StatusConflict, "container sudah berjalan")
		return
	}
	args := append(append([]string{}, argsAwal...), id)
	if _, err := s.dockerRun(sess.Username, "", args...); err != nil {
		writeHelperErr(w, err)
		return
	}
	s.store.LogActivity(sess.Username, "docker_container_"+action, "aksi container",
		map[string]any{"container": id}, clientIP(r))
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) containerBerjalan(username, id string) bool {
	res, err := s.dockerRun(username, "", "inspect", "-f", "{{.State.Running}}", id)
	if err != nil {
		return false
	}
	return strings.TrimSpace(res.Stdout) == "true"
}

// maxTailLog membatasi jumlah baris log yang boleh diminta sekali panggil —
// tanpa batas, satu klik bisa menarik ratusan MB log ke browser.
const maxTailLog = 2000

func (s *Server) handleContainerLogs(w http.ResponseWriter, r *http.Request) {
	if !requireSudo(w, r) {
		return
	}
	sess := sessionFrom(r)
	id := chi.URLParam(r, "id")
	if id == "" {
		writeErr(w, http.StatusBadRequest, "id container kosong")
		return
	}
	tail := queryInt(r, "tail", 200)
	if tail < 1 {
		tail = 1
	}
	if tail > maxTailLog {
		tail = maxTailLog
	}
	res, err := s.dockerRun(sess.Username, "", "logs", "--tail", strconv.Itoa(tail), id)
	if err != nil {
		writeHelperErr(w, err)
		return
	}
	// Docker menulis log aplikasi ke stdout DAN stderr; keduanya bagian dari
	// log container, jadi ditampilkan berurutan apa adanya.
	writeJSON(w, http.StatusOK, map[string]any{
		"container": id,
		"tail":      tail,
		"content":   res.Stdout + res.Stderr,
	})
}

// ---- Compose stacks ----

// slugProyek mengubah nama stack jadi nama project docker compose: huruf
// kecil, tanpa spasi dan tanpa simbol. Nama project itulah yang jadi awalan
// nama container (`<project>-<service>-<index>`), jadi stack bernama "cctv"
// menghasilkan `cctv-agentdvr-1` alih-alih `agentdvr-agentdvr-1` yang
// diturunkan compose dari nama folder.
//
// ponytail: pemisah ikut dibuang, bukan diganti "-", sesuai permintaan
// "tanpa spasi maupun simbol". Konsekuensinya dua stack bernama "web 1" dan
// "web-1" menghasilkan project yang sama. Jalan naiknya kalau itu jadi
// masalah nyata: pertahankan "-" sebagai pemisah (compose menerimanya) atau
// tolak nama yang slug-nya bentrok saat stack didaftarkan.
func slugProyek(nama string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(nama) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

type composeLsRow struct {
	Name        string
	Status      string
	ConfigFiles string
}

// daftarComposeLs membaca seluruh project compose yang dikenal Docker,
// termasuk yang container-nya sedang berhenti (--all).
func (s *Server) daftarComposeLs(username string) []composeLsRow {
	res, err := s.dockerRun(username, "", "compose", "ls", "--all", "--format", "json")
	if err != nil {
		return nil
	}
	var rows []composeLsRow
	if err := json.Unmarshal([]byte(strings.TrimSpace(res.Stdout)), &rows); err != nil {
		return nil
	}
	return rows
}

// proyekBerjalan memetakan berkas compose → nama project yang saat ini
// memegangnya di Docker.
func proyekBerjalan(rows []composeLsRow) map[string]string {
	m := map[string]string{}
	for _, r := range rows {
		// ConfigFiles bisa memuat beberapa berkas dipisah koma.
		for _, cfg := range strings.Split(r.ConfigFiles, ",") {
			cfg = filepath.Clean(strings.TrimSpace(cfg))
			if cfg == "" || cfg == "." {
				continue
			}
			m[cfg] = r.Name
		}
	}
	return m
}

// argsCompose menyusun awalan `compose -p <project> -f <berkas>` untuk satu
// stack terdaftar.
//
// Project yang SUDAH memegang berkas compose ini menang atas slug nama stack.
// Tanpa aturan itu, stack yang terlanjur jalan di bawah nama bawaan compose
// (diturunkan dari nama folder) akan hilang dari panel begitu versi ini
// dipasang: `ps` menanyakan project baru dan mendapat nol container, `down`
// tidak menyentuh apa pun, dan `up` justru menyalakan set container KEDUA
// yang langsung bentrok port dengan yang lama. Dengan aturan ini stack lama
// tetap dikelola apa adanya, dan pindah ke nama baru dengan sendirinya
// setelah sekali `down` — saat itu tidak ada lagi project yang memegangnya.
func argsCompose(st store.Stack, pemegang map[string]string) []string {
	proyek := slugProyek(st.Name)
	if lama := pemegang[filepath.Clean(st.ComposePath)]; lama != "" {
		proyek = lama
	}
	if proyek == "" {
		// Nama stack tidak menyisakan satu pun karakter yang sah (mis. hanya
		// simbol). Biarkan compose menentukan sendiri, jangan kirim -p kosong.
		return []string{"compose", "-f", st.ComposePath}
	}
	return []string{"compose", "-p", proyek, "-f", st.ComposePath}
}

type stackView struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	ComposePath string `json:"compose_path"`
	Description string `json:"description"`
	// Status selalu dibaca live dari `docker compose ps`, tidak pernah
	// disimpan di DB — supaya tidak ada state basi.
	Running int    `json:"running"`
	Total   int    `json:"total"`
	Error   string `json:"error,omitempty"`
	// External = stack yang sudah jalan di Docker tapi belum terdaftar di
	// panel (mis. dibuat manual atau oleh tool lain). ID-nya 0; UI menawarkan
	// "Daftarkan" alih-alih tombol kelola.
	External bool `json:"external,omitempty"`
}

// stackKomponen adalah stack compose yang dibuat KOMPONEN panel, bukan oleh
// user. Stack seperti ini tidak boleh muncul sebagai "belum terdaftar" dan
// menunggu user menekan Daftarkan: panel sendiri yang membuat berkas
// compose-nya, jadi panel juga yang tahu di mana ia berada.
var stackKomponen = []struct{ nama, compose, ket string }{
	{
		"supabase",
		"/opt/supabase/supabase-project/docker-compose.yml",
		"Dipasang dari Settings → Components.",
	},
}

// sinkronStackKomponen mendaftarkan stack milik komponen panel yang belum
// terdaftar, dan membuang pendaftarannya lagi begitu komponennya dicopot.
//
// Dijalankan dari handleStackList — sebuah GET yang karena itu menulis, dan
// itu disengaja. Mendaftarkan hanya pada saat instalasi tidak cukup: mesin
// yang sudah memasang komponennya lebih dulu (termasuk lewat rilis panel
// sebelum ada pendaftaran ini) tidak akan pernah melewati jalur instalasi
// lagi, jadi stack-nya akan selamanya berdiri di daftar "belum terdaftar"
// tanpa satu pun tombol di panel yang bisa membereskannya. Operasinya
// idempoten dan dikunci pada path berkas compose, bukan nama — stack yang
// sudah diganti namanya user tetap dikenali sebagai stack yang sama.
func (s *Server) sinkronStackKomponen(stacks []store.Stack) []store.Stack {
	berubah := false
	for _, k := range stackKomponen {
		compose := filepath.Clean(k.compose)
		var terdaftar *store.Stack
		for i := range stacks {
			if filepath.Clean(stacks[i].ComposePath) == compose {
				terdaftar = &stacks[i]
				break
			}
		}
		ada := berkasAda(compose)
		switch {
		case ada && terdaftar == nil:
			if _, err := s.store.AddStack(k.nama, compose, k.ket); err != nil {
				// Daftar stack lebih penting daripada pendaftaran otomatisnya:
				// kegagalan di sini hanya mengembalikan keadaan lama (muncul
				// sebagai stack "belum terdaftar"), bukan halaman yang kosong.
				log.Printf("stack %s: gagal didaftarkan otomatis: %v", k.nama, err)
				continue
			}
		case !ada && terdaftar != nil:
			// Komponennya dicopot: barisnya menunjuk berkas yang tidak ada
			// lagi, dan membiarkannya berarti satu kartu permanen yang setiap
			// kali melapor error dari `docker compose ps`.
			if err := s.store.DeleteStack(terdaftar.ID); err != nil {
				log.Printf("stack %s: gagal dihapus dari daftar: %v", k.nama, err)
				continue
			}
		default:
			continue
		}
		berubah = true
	}
	if !berubah {
		return stacks
	}
	baru, err := s.store.Stacks()
	if err != nil {
		return stacks
	}
	return baru
}

// berkasAda menjawab apakah path menunjuk berkas biasa yang ada.
func berkasAda(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && !fi.IsDir()
}

func (s *Server) handleStackList(w http.ResponseWriter, r *http.Request) {
	if !requireSudo(w, r) {
		return
	}
	sess := sessionFrom(r)
	stacks, err := s.store.Stacks()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	stacks = s.sinkronStackKomponen(stacks)
	// Satu kali `compose ls` untuk seluruh daftar: dipakai menentukan project
	// tiap stack terdaftar DAN menemukan stack yang belum terdaftar.
	lsRows := s.daftarComposeLs(sess.Username)
	pemegang := proyekBerjalan(lsRows)
	out := make([]stackView, 0, len(stacks))
	for _, st := range stacks {
		v := stackView{ID: st.ID, Name: st.Name, ComposePath: st.ComposePath, Description: st.Description}
		args := append(argsCompose(st, pemegang), "ps", "--format", "{{json .}}")
		res, err := s.dockerRun(sess.Username, filepath.Dir(st.ComposePath), args...)
		if err != nil {
			v.Error = err.Error()
		} else {
			for _, line := range strings.Split(strings.TrimSpace(res.Stdout), "\n") {
				if line == "" {
					continue
				}
				var row struct{ State string }
				if err := json.Unmarshal([]byte(line), &row); err != nil {
					continue
				}
				v.Total++
				if row.State == "running" {
					v.Running++
				}
			}
		}
		out = append(out, v)
	}
	// Stack yang sudah hidup di Docker tapi belum terdaftar ikut ditampilkan.
	// Tanpa ini panel terlihat kosong padahal servernya penuh stack berjalan,
	// dan user mendaftarkan ulang compose yang sama sampai bentrok.
	out = append(out, stackLuar(lsRows, stacks)...)
	writeJSON(w, http.StatusOK, out)
}

// stackLuar menyaring hasil `docker compose ls` menjadi stack yang path
// compose-nya BELUM terdaftar di panel.
func stackLuar(rows []composeLsRow, terdaftar []store.Stack) []stackView {
	sudah := map[string]bool{}
	for _, st := range terdaftar {
		sudah[filepath.Clean(st.ComposePath)] = true
		sudah[st.Name] = true
	}
	var out []stackView
	for _, r := range rows {
		// ConfigFiles bisa berisi beberapa file dipisah koma; yang pertama
		// sudah cukup untuk `docker compose -f`.
		cfg := r.ConfigFiles
		if i := strings.IndexByte(cfg, ','); i >= 0 {
			cfg = cfg[:i]
		}
		cfg = filepath.Clean(strings.TrimSpace(cfg))
		if cfg == "" || cfg == "." || sudah[cfg] || sudah[r.Name] {
			continue
		}
		v := stackView{Name: r.Name, ComposePath: cfg, External: true, Description: r.Status}
		v.Running, v.Total = parseComposeStatus(r.Status)
		out = append(out, v)
	}
	return out
}

// parseComposeStatus membaca ringkasan `docker compose ls`, mis.
// "running(4)" atau "running(2), exited(1)".
func parseComposeStatus(status string) (running, total int) {
	for _, bagian := range strings.Split(status, ",") {
		bagian = strings.TrimSpace(bagian)
		buka := strings.IndexByte(bagian, '(')
		if buka < 0 || !strings.HasSuffix(bagian, ")") {
			continue
		}
		n, err := strconv.Atoi(bagian[buka+1 : len(bagian)-1])
		if err != nil {
			continue
		}
		total += n
		if strings.HasPrefix(bagian, "running") {
			running += n
		}
	}
	return running, total
}

type stackBody struct {
	Name        string `json:"name"`
	ComposePath string `json:"compose_path"`
	Description string `json:"description"`
}

func (s *Server) handleStackCreate(w http.ResponseWriter, r *http.Request) {
	if !requireSudo(w, r) {
		return
	}
	sess := sessionFrom(r)
	var body stackBody
	if err := decodeBody(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if body.Name == "" {
		writeErr(w, http.StatusBadRequest, "Nama stack wajib diisi")
		return
	}
	if !filepath.IsAbs(body.ComposePath) {
		writeErr(w, http.StatusBadRequest, "Path docker-compose.yml harus absolut")
		return
	}
	st, err := s.store.AddStack(body.Name, filepath.Clean(body.ComposePath), body.Description)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.store.LogActivity(sess.Username, "docker_stack_create", "daftar stack",
		map[string]any{"name": st.Name, "path": st.ComposePath}, clientIP(r))
	writeJSON(w, http.StatusCreated, st)
}

func (s *Server) handleStackUpdate(w http.ResponseWriter, r *http.Request) {
	if !requireSudo(w, r) {
		return
	}
	sess := sessionFrom(r)
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "id tidak valid")
		return
	}
	var body stackBody
	if err := decodeBody(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if body.Name == "" {
		writeErr(w, http.StatusBadRequest, "Nama stack wajib diisi")
		return
	}
	if !filepath.IsAbs(body.ComposePath) {
		writeErr(w, http.StatusBadRequest, "Path docker-compose.yml harus absolut")
		return
	}
	if err := s.store.UpdateStack(id, body.Name, filepath.Clean(body.ComposePath), body.Description); err != nil {
		writeErr(w, http.StatusNotFound, "stack tidak ditemukan")
		return
	}
	s.store.LogActivity(sess.Username, "docker_stack_update", "ubah stack",
		map[string]any{"id": id, "name": body.Name, "path": body.ComposePath}, clientIP(r))
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleStackDelete(w http.ResponseWriter, r *http.Request) {
	if !requireSudo(w, r) {
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "id tidak valid")
		return
	}
	if err := s.store.DeleteStack(id); err != nil {
		writeErr(w, http.StatusNotFound, "stack tidak ditemukan")
		return
	}
	// Hanya registrasinya yang dihapus — container yang sedang jalan tidak
	// disentuh, supaya penghapusan dari daftar tidak diam-diam mematikan service.
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

var stackActions = map[string][]string{
	"up":      {"up", "-d"},
	"down":    {"down"},
	"restart": {"restart"},
	"stop":    {"stop"},
	"start":   {"start"},
	"pull":    {"pull"},
}

func (s *Server) handleStackAction(w http.ResponseWriter, r *http.Request) {
	if !requireSudo(w, r) {
		return
	}
	sess := sessionFrom(r)
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "id tidak valid")
		return
	}
	action := chi.URLParam(r, "action")
	extra, ok := stackActions[action]
	if !ok {
		writeErr(w, http.StatusBadRequest, "aksi stack tidak dikenal")
		return
	}
	st, err := s.store.Stack(id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "stack tidak ditemukan")
		return
	}
	args := append(argsCompose(st, proyekBerjalan(s.daftarComposeLs(sess.Username))), extra...)
	res, err := s.dockerRun(sess.Username, filepath.Dir(st.ComposePath), args...)
	if err != nil {
		writeHelperErr(w, err)
		return
	}
	s.store.LogActivity(sess.Username, "docker_stack_"+action, action,
		map[string]any{"stack": st.Name}, clientIP(r))
	writeJSON(w, http.StatusOK, map[string]string{
		"status": "ok", "output": strings.TrimSpace(res.Stdout + res.Stderr),
	})
}

// maxEnvBytes membatasi ukuran file .env yang dibaca ke memori — file env
// yang wajar hanya beberapa KB.
const maxEnvBytes = 256 << 10

func (s *Server) handleStackEnvGet(w http.ResponseWriter, r *http.Request) {
	if !requireSudo(w, r) {
		return
	}
	sess := sessionFrom(r)
	st, ok := s.stackFromURL(w, r)
	if !ok {
		return
	}
	envPath := filepath.Join(filepath.Dir(st.ComposePath), ".env")
	stream, err := s.helper.Stream(helperproto.CmdFileRead, sess.Username,
		helperproto.PathArgs{Path: envPath})
	if err != nil {
		// .env opsional — belum ada bukan error.
		writeJSON(w, http.StatusOK, map[string]string{"path": envPath, "content": ""})
		return
	}
	defer stream.Close()
	content, _ := io.ReadAll(io.LimitReader(stream, maxEnvBytes))
	writeJSON(w, http.StatusOK, map[string]string{"path": envPath, "content": string(content)})
}

type envBody struct {
	Content string `json:"content"`
}

// dirSistem menandai direktori milik sistem yang kepemilikannya TIDAK boleh
// diserahkan ke siapa pun, betapa pun sah permintaannya.
//
// Bukan kekhawatiran teoretis: penyerahan otomatis di bawah ikut menyerahkan
// DIREKTORI yang memuat berkas compose, dan stack yang didaftarkan dengan
// compose_path `/etc/docker-compose.yml` akan membuat `/etc` berpindah pemilik
// hanya karena seseorang menekan Simpan. Berkasnya sendiri tetap diserahkan,
// jadi menyunting berkas yang sudah ada tetap bekerja — yang hilang cuma
// kemampuan membuat berkas baru di direktori yang memang bukan milik stack.
func dirSistem(p string) bool {
	switch filepath.Clean(p) {
	case "/", "/etc", "/usr", "/var", "/opt", "/srv", "/home", "/root", "/tmp",
		"/mnt", "/media", "/boot", "/lib", "/lib64", "/bin", "/sbin",
		"/run", "/dev", "/proc", "/sys":
		return true
	}
	return false
}

// serahkanKonfigStack menyerahkan kepemilikan berkas konfigurasi sebuah stack
// — berikut direktori yang memuatnya — ke sudoer yang sedang menyimpannya.
//
// Perlu ada karena dua keputusan panel yang sama-sama benar bertabrakan:
// helper daemon MEMBUAT berkas sebagai root (setup.sh Supabase, misalnya),
// sementara panel MENULIS berkas sebagai user yang login — worker-nya
// berjalan dengan kredensial user supaya kernel yang menegakkan izinnya.
// Hasilnya "permission denied" untuk berkas yang justru dibuat panel itu
// sendiri, dan tidak ada satu pun tombol di panel yang bisa membereskannya:
// halaman File Manager pun menulis sebagai user, dan chown tidak punya UI.
//
// Direktorinya ikut diserahkan, bukan hanya berkasnya: penyimpanan compose
// menulis berkas sementara di sebelahnya lalu memindahkannya, dan itu menuntut
// izin tulis pada DIREKTORI, bukan pada berkas lama.
//
// HanyaMilikRoot menjaga supaya ini tidak pernah jadi pengambilalihan: berkas
// yang sudah milik admin lain didiamkan, dan penyimpanannya gagal seperti
// sebelumnya — dengan pesan izin yang memang benar.
func (s *Server) serahkanKonfigStack(username string, path ...string) {
	for _, p := range path {
		if p == "" || dirSistem(p) {
			continue
		}
		if err := s.helper.Call(helperproto.CmdFileChown, username,
			helperproto.ChownArgs{Path: p, Owner: username, HanyaMilikRoot: true}, nil); err != nil {
			// Bukan alasan membatalkan penyimpanan: kalau memang tidak bisa,
			// penulisannya sendiri yang akan melapor dengan pesan yang tepat.
			log.Printf("stack: kepemilikan %s tidak diserahkan ke %s: %v", p, username, err)
		}
	}
}

func (s *Server) handleStackEnvSet(w http.ResponseWriter, r *http.Request) {
	if !requireSudo(w, r) {
		return
	}
	sess := sessionFrom(r)
	st, ok := s.stackFromURL(w, r)
	if !ok {
		return
	}
	var body envBody
	if err := decodeBody(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	envPath := filepath.Join(filepath.Dir(st.ComposePath), ".env")
	s.serahkanKonfigStack(sess.Username, filepath.Dir(st.ComposePath), envPath)
	stream, err := s.helper.Stream(helperproto.CmdFileWrite, sess.Username,
		helperproto.WriteArgs{Path: envPath})
	if err != nil {
		writeHelperErr(w, err)
		return
	}
	_, werr := io.WriteString(stream, body.Content)
	doneErr := stream.Selesai()
	stream.Close()
	if werr != nil {
		writeErr(w, http.StatusInternalServerError, "gagal menulis .env: "+werr.Error())
		return
	}
	if doneErr != nil {
		writeHelperErr(w, doneErr)
		return
	}
	s.store.LogActivity(sess.Username, "docker_stack_env", "ubah .env",
		map[string]any{"stack": st.Name, "path": envPath}, clientIP(r))
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "path": envPath})
}

// maxComposeBytes: file compose yang wajar hanya beberapa puluh KB.
const maxComposeBytes = 512 << 10

func (s *Server) handleStackComposeGet(w http.ResponseWriter, r *http.Request) {
	if !requireSudo(w, r) {
		return
	}
	sess := sessionFrom(r)
	st, ok := s.stackFromURL(w, r)
	if !ok {
		return
	}
	// Path TIDAK diambil dari request — hanya dari baris stack di SQLite, supaya
	// endpoint ini tidak bisa dipakai membaca file sembarangan.
	stream, err := s.helper.Stream(helperproto.CmdFileRead, sess.Username,
		helperproto.PathArgs{Path: st.ComposePath})
	if err != nil {
		writeHelperErr(w, err)
		return
	}
	defer stream.Close()
	content, _ := io.ReadAll(io.LimitReader(stream, maxComposeBytes))
	writeJSON(w, http.StatusOK, map[string]string{"path": st.ComposePath, "content": string(content)})
}

func (s *Server) handleStackComposeSet(w http.ResponseWriter, r *http.Request) {
	if !requireSudo(w, r) {
		return
	}
	sess := sessionFrom(r)
	st, ok := s.stackFromURL(w, r)
	if !ok {
		return
	}
	var body envBody
	if err := decodeBody(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(body.Content) == "" {
		writeErr(w, http.StatusBadRequest, "isi compose tidak boleh kosong")
		return
	}

	// Cadangannya ikut diserahkan: `.bak` milik root yang sudah ada akan
	// menolak ditimpa oleh salinan berikutnya, dan kegagalan itu terjadi
	// SESUDAH compose barunya lolos validasi.
	s.serahkanKonfigStack(sess.Username,
		filepath.Dir(st.ComposePath), st.ComposePath, st.ComposePath+".bak")

	// Urutan wajib: tulis ke file sementara → validasi → backup → ganti.
	// Menulis langsung ke file asli berarti compose yang salah ketik sudah
	// merusak stack sebelum sempat divalidasi.
	tmp := st.ComposePath + ".lindash-tmp"
	if err := s.tulisFile(sess.Username, tmp, body.Content); err != nil {
		writeErr(w, http.StatusInternalServerError, "gagal menulis file sementara: "+err.Error())
		return
	}
	if res, err := s.dockerRun(sess.Username, filepath.Dir(st.ComposePath),
		"compose", "-f", tmp, "config", "-q"); err != nil {
		_ = s.helper.Call(helperproto.CmdFileRemove, sess.Username, helperproto.PathArgs{Path: tmp}, nil)
		pesan := strings.TrimSpace(res.Stderr)
		if pesan == "" {
			pesan = err.Error()
		}
		writeErr(w, http.StatusBadRequest, "compose ditolak Docker: "+pesan)
		return
	}
	// Backup versi lama sebelum ditimpa; menimpa backup sebelumnya disengaja.
	_ = s.helper.Call(helperproto.CmdFileCopy, sess.Username,
		helperproto.TwoPathArgs{Source: st.ComposePath, Dest: st.ComposePath + ".bak"}, nil)
	if err := s.helper.Call(helperproto.CmdFileMove, sess.Username,
		helperproto.TwoPathArgs{Source: tmp, Dest: st.ComposePath}, nil); err != nil {
		writeHelperErr(w, err)
		return
	}
	s.store.LogActivity(sess.Username, "docker_stack_compose", "ubah docker-compose.yml",
		map[string]any{"stack": st.Name, "path": st.ComposePath}, clientIP(r))
	// Menyimpan tidak memicu deploy — frontend memakai flag ini untuk
	// mengingatkan bahwa perubahan baru berlaku setelah Up/Restart.
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok", "path": st.ComposePath, "needs_redeploy": true,
	})
}

func (s *Server) tulisFile(username, path, content string) error {
	stream, err := s.helper.Stream(helperproto.CmdFileWrite, username, helperproto.WriteArgs{Path: path})
	if err != nil {
		return err
	}
	_, werr := io.WriteString(stream, content)
	cerr := stream.Selesai()
	stream.Close()
	if werr != nil {
		return werr
	}
	return cerr
}

func (s *Server) stackFromURL(w http.ResponseWriter, r *http.Request) (stackRecord, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "id tidak valid")
		return stackRecord{}, false
	}
	st, err := s.store.Stack(id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "stack tidak ditemukan")
		return stackRecord{}, false
	}
	return stackRecord{ID: st.ID, Name: st.Name, ComposePath: st.ComposePath}, true
}

type stackRecord struct {
	ID          int64
	Name        string
	ComposePath string
}

// ---- Sumber daya Docker selain container: image, volume, network ----------
//
// Ketiganya dibaca dengan `--format {{json .}}` dan diteruskan apa adanya ke
// UI. Ukuran volume TIDAK ikut dibaca: satu-satunya sumbernya adalah
// `docker system df -v`, yang menghitung ulang seluruh disk tiap panggilan dan
// pada host dengan banyak volume butuh belasan detik.
// ponytail: tanpa ukuran volume; tambahkan lewat `system df -v` di endpoint
// terpisah kalau memang diminta, jangan di jalur daftar ini.

type dockerImage struct {
	ID         string `json:"id"`
	Repository string `json:"repository"`
	Tag        string `json:"tag"`
	Size       string `json:"size"`
	Created    string `json:"created"`
	// Dangling = image tanpa tag, sisa build atau pull yang tergantikan. Ini
	// yang dibuang `image prune`, jadi UI perlu bisa menandainya.
	Dangling bool `json:"dangling"`
}

type dockerVolume struct {
	Name       string `json:"name"`
	Driver     string `json:"driver"`
	Mountpoint string `json:"mountpoint"`
}

type dockerNetwork struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Driver   string `json:"driver"`
	Scope    string `json:"scope"`
	Internal bool   `json:"internal"`
	// Bawaan docker (bridge/host/none) tidak bisa dihapus dan percobaannya
	// hanya menghasilkan error; UI menyembunyikan tombol hapusnya.
	Builtin bool `json:"builtin"`
}

// boolCLI menerima "true"/"false" — bentuk yang dipakai `docker network ls
// --format {{json .}}`, yang mengirim SEMUA field sebagai string — maupun
// boolean JSON asli. Tanpa ini, satu perubahan format di docker membuat
// json.Unmarshal seluruh barisnya gagal, dan barisnya hilang dari daftar
// tanpa satu pun pesan: network yang jelas ada dilaporkan tidak ada.
type boolCLI bool

func (b *boolCLI) UnmarshalJSON(p []byte) error {
	*b = boolCLI(strings.Trim(string(p), `"`) == "true")
	return nil
}

// barisJSON memecah keluaran `--format {{json .}}` yang satu objek per baris
// (bukan satu array) lalu men-decode tiap barisnya ke v lewat fn.
func barisJSON(out string, fn func(line []byte)) {
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		fn([]byte(line))
	}
}

func (s *Server) handleDockerImages(w http.ResponseWriter, r *http.Request) {
	if !requireSudo(w, r) {
		return
	}
	res, err := s.dockerRun(sessionFrom(r).Username, "", "image", "ls", "--format", "{{json .}}")
	if err != nil {
		writeHelperErr(w, err)
		return
	}
	out := []dockerImage{}
	barisJSON(res.Stdout, func(line []byte) {
		var row struct{ ID, Repository, Tag, Size, CreatedSince string }
		if json.Unmarshal(line, &row) != nil {
			return
		}
		out = append(out, dockerImage{
			ID: row.ID, Repository: row.Repository, Tag: row.Tag,
			Size: row.Size, Created: row.CreatedSince,
			Dangling: row.Repository == "<none>" || row.Tag == "<none>",
		})
	})
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleDockerVolumes(w http.ResponseWriter, r *http.Request) {
	if !requireSudo(w, r) {
		return
	}
	res, err := s.dockerRun(sessionFrom(r).Username, "", "volume", "ls", "--format", "{{json .}}")
	if err != nil {
		writeHelperErr(w, err)
		return
	}
	out := []dockerVolume{}
	barisJSON(res.Stdout, func(line []byte) {
		var row struct{ Name, Driver, Mountpoint string }
		if json.Unmarshal(line, &row) != nil {
			return
		}
		out = append(out, dockerVolume{Name: row.Name, Driver: row.Driver, Mountpoint: row.Mountpoint})
	})
	writeJSON(w, http.StatusOK, out)
}

// networkBawaan: tiga network yang dibuat docker sendiri saat daemon start.
// Menghapusnya ditolak daemon, jadi tombolnya tidak perlu ada.
var networkBawaan = map[string]bool{"bridge": true, "host": true, "none": true}

func (s *Server) handleDockerNetworks(w http.ResponseWriter, r *http.Request) {
	if !requireSudo(w, r) {
		return
	}
	res, err := s.dockerRun(sessionFrom(r).Username, "", "network", "ls", "--format", "{{json .}}")
	if err != nil {
		writeHelperErr(w, err)
		return
	}
	out := []dockerNetwork{}
	barisJSON(res.Stdout, func(line []byte) {
		var row struct {
			ID, Name, Driver, Scope string
			Internal                boolCLI
		}
		if json.Unmarshal(line, &row) != nil {
			return
		}
		out = append(out, dockerNetwork{
			ID: row.ID, Name: row.Name, Driver: row.Driver, Scope: row.Scope,
			Internal: bool(row.Internal), Builtin: networkBawaan[row.Name],
		})
	})
	writeJSON(w, http.StatusOK, out)
}

// ---- Pemakaian disk ------------------------------------------------------
//
// Menjawab pertanyaan yang tidak bisa dijawab tabel image/volume/network:
// dari sekian puluh GB yang dipakai Docker, berapa yang sebenarnya masih
// dipakai dan berapa yang bisa dibebaskan. Tanpa angka itu, tombol Bersihkan
// adalah tebakan — dan `image prune` polos yang cuma membuang image dangling
// sering mengembalikan 0 B di host yang jelas penuh, yang terbaca sebagai
// tombol rusak.
//
// Sumbernya `docker system df` (tanpa -v). Versi -v yang merinci per-image
// dan per-volume sengaja tidak dipakai: itu yang mahal di host dengan banyak
// volume, dan yang dibutuhkan halaman ini hanya empat baris ringkasan.
type dockerDiskUsage struct {
	// Type: "Images" | "Containers" | "Local Volumes" | "Build Cache" —
	// ditulis docker sendiri, dipakai UI untuk memilih label dan tombolnya.
	Type string `json:"type"`
	// Semua nilai di bawah adalah string apa adanya dari docker ("12",
	// "3.38GB", "1.2GB (35%)"). Sengaja tidak diurai jadi angka: yang
	// ditampilkan panel persis yang ditampilkan `docker system df` di
	// terminal, jadi tidak ada dua sumber kebenaran soal satuan dan
	// pembulatan.
	Total       string `json:"total"`
	Active      string `json:"active"`
	Size        string `json:"size"`
	Reclaimable string `json:"reclaimable"`
}

func (s *Server) handleDockerDiskUsage(w http.ResponseWriter, r *http.Request) {
	if !requireSudo(w, r) {
		return
	}
	// `system df` menjalankan template SEKALI PER JENIS, jadi keluarannya
	// empat objek JSON berbaris — bentuk yang sama dengan `ls --format`.
	res, err := s.dockerRun(sessionFrom(r).Username, "", "system", "df", "--format", "{{json .}}")
	if err != nil {
		writeHelperErr(w, err)
		return
	}
	out := []dockerDiskUsage{}
	barisJSON(res.Stdout, func(line []byte) {
		var row struct{ Type, TotalCount, Active, Size, Reclaimable string }
		if json.Unmarshal(line, &row) != nil || row.Type == "" {
			return
		}
		out = append(out, dockerDiskUsage{
			Type: row.Type, Total: row.TotalCount, Active: row.Active,
			Size: row.Size, Reclaimable: row.Reclaimable,
		})
	})
	writeJSON(w, http.StatusOK, out)
}

// dayaDocker memetakan segmen URL ke subcommand docker. Nilai dari URL tidak
// pernah diteruskan langsung, sama seperti aksiContainer.
//
// buildcache hanya bisa di-prune, tidak punya daftar dan tidak punya entri
// yang dihapus satu per satu — lihat handleDockerDayaDelete.
var dayaDocker = map[string]string{
	"images":     "image",
	"volumes":    "volume",
	"networks":   "network",
	"buildcache": "builder",
}

func (s *Server) handleDockerDayaDelete(w http.ResponseWriter, r *http.Request) {
	if !requireSudo(w, r) {
		return
	}
	sess := sessionFrom(r)
	daya, ok := dayaDocker[chi.URLParam(r, "daya")]
	if !ok {
		writeErr(w, http.StatusBadRequest, "sumber daya docker tidak dikenal")
		return
	}
	// Cache build tidak punya entri yang bisa dihapus satu per satu dari
	// panel. Ditolak dengan kalimat yang menyebut apa yang harus dipakai
	// sebagai gantinya, bukan dibiarkan jatuh ke whitelist helper yang akan
	// menjawab "subcommand builder \"rm\" tidak diizinkan".
	if daya == "builder" {
		writeErr(w, http.StatusBadRequest,
			"cache build tidak dihapus satu per satu — pakai tombol Bersihkan pada baris Build Cache")
		return
	}
	id := chi.URLParam(r, "id")
	if id == "" {
		writeErr(w, http.StatusBadRequest, "id kosong")
		return
	}
	// Network bawaan ditolak di sini, bukan diserahkan ke daemon: pesan
	// docker untuk kasus ini menyebut "pre-defined network" tanpa menjelaskan
	// bahwa itu memang tidak bisa diubah sama sekali.
	if daya == "network" && networkBawaan[id] {
		writeErr(w, http.StatusBadRequest, "network bawaan docker tidak bisa dihapus")
		return
	}
	// Tanpa -f: daemon menolak menghapus image/volume/network yang masih
	// dipakai, dan penolakan itu justru pengaman yang paling berguna di sini.
	// Memaksanya berarti container yang sedang jalan kehilangan datanya.
	if _, err := s.dockerRun(sess.Username, "", daya, "rm", id); err != nil {
		writeHelperErr(w, err)
		return
	}
	s.store.LogActivity(sess.Username, "docker_"+daya+"_remove", "hapus "+daya,
		map[string]any{daya: id}, clientIP(r))
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleDockerDayaPrune(w http.ResponseWriter, r *http.Request) {
	if !requireSudo(w, r) {
		return
	}
	sess := sessionFrom(r)
	daya, ok := dayaDocker[chi.URLParam(r, "daya")]
	if !ok {
		writeErr(w, http.StatusBadRequest, "sumber daya docker tidak dikenal")
		return
	}
	// -f melewati pertanyaan konfirmasi CLI (tidak ada yang bisa menjawabnya
	// di sini); konfirmasi sebenarnya sudah diminta di UI.
	//
	// `image prune` polos hanya membuang image dangling — yang tidak punya tag
	// sama sekali — sedangkan `-a` membuang setiap image yang tidak dipakai
	// container mana pun, termasuk image stack yang sedang `down` (down
	// menghapus container-nya, jadi image-nya tidak lagi terpakai) yang lalu
	// harus diunduh ulang. Selisih itu terlalu besar untuk satu tombol yang
	// sama, jadi keduanya jadi dua aksi terpisah dengan dua kalimat
	// konfirmasi sendiri — bukan satu tombol yang diam-diam memilih.
	args := []string{daya, "prune", "-f"}
	semua := daya == "image" && r.URL.Query().Get("semua") == "1"
	if semua {
		args = append(args, "-a")
	}
	res, err := s.dockerRun(sess.Username, "", args...)
	if err != nil {
		writeHelperErr(w, err)
		return
	}
	s.store.LogActivity(sess.Username, "docker_"+daya+"_prune", "prune "+daya,
		map[string]any{"semua": semua}, clientIP(r))
	writeJSON(w, http.StatusOK, map[string]string{
		"status": "ok",
		// Baris "Total reclaimed space: ..." dari docker dikirim apa adanya —
		// angka yang dihasilkan aksi ini adalah satu-satunya jawaban yang
		// dicari user setelah menekan tombolnya.
		"output": strings.TrimSpace(res.Stdout),
	})
}
