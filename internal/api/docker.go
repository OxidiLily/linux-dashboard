package api

import (
	"encoding/json"
	"io"
	"net/http"
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
	out := make([]stackView, 0, len(stacks))
	for _, st := range stacks {
		v := stackView{ID: st.ID, Name: st.Name, ComposePath: st.ComposePath, Description: st.Description}
		res, err := s.dockerRun(sess.Username, filepath.Dir(st.ComposePath),
			"compose", "-f", st.ComposePath, "ps", "--format", "{{json .}}")
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
	out = append(out, s.stackLuar(sess.Username, stacks)...)
	writeJSON(w, http.StatusOK, out)
}

// stackLuar membaca `docker compose ls` dan menyaring yang path compose-nya
// sudah terdaftar di panel.
func (s *Server) stackLuar(username string, terdaftar []store.Stack) []stackView {
	res, err := s.dockerRun(username, "", "compose", "ls", "--all", "--format", "json")
	if err != nil {
		return nil
	}
	var rows []struct {
		Name        string
		Status      string
		ConfigFiles string
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(res.Stdout)), &rows); err != nil {
		return nil
	}
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
	args := append([]string{"compose", "-f", st.ComposePath}, extra...)
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
