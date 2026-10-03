package api

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"linux-dashboard/OxidiLily/internal/helperclient"
	"linux-dashboard/OxidiLily/internal/helperproto"
	"linux-dashboard/OxidiLily/internal/store"
	"linux-dashboard/OxidiLily/internal/terminal"
)

// sesiPeriksa: seberapa sering koneksi WebSocket yang sudah terbuka memeriksa
// ulang sesi pemiliknya. Variabel, bukan konstanta, supaya perilakunya bisa
// diuji tanpa menunggu 15 detik.
var sesiPeriksa = 5 * time.Second

// pantauSesi menutup koneksi WebSocket begitu sesinya tidak lagi sah (logout,
// password diganti, akun dihapus, atau TTL habis).
//
// Handshake hanya memeriksa sesi satu kali. Tanpa pemantauan, cookie yang
// dicuri tetap memberi terminal shell yang hidup walaupun korban sudah logout
// atau mengganti password — dan itu justru sesi yang paling berharga untuk
// dipertahankan penyerang. Pemeriksaan berkala (bukan per pesan) dipilih supaya
// tidak menambah satu query ke setiap ketikan di terminal.
//
// Fungsi yang dikembalikan menghentikan pemantauan; aman dipanggil lebih dari
// sekali.
func (s *Server) pantauSesi(ctx context.Context, sess store.Session, tutup func()) func() {
	done := make(chan struct{})
	var sekali sync.Once
	stop := func() { sekali.Do(func() { close(done) }) }
	go func() {
		t := time.NewTicker(sesiPeriksa)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-done:
				return
			case <-t.C:
				if _, ok := s.store.GetSession(sess.ID); !ok {
					tutup()
					return
				}
			}
		}
	}()
	return stop
}

// acceptOptions: koneksi WebSocket harus berasal dari halaman dashboard itu
// sendiri. Origin dicek default oleh library terhadap Host request.
var acceptOptions = &websocket.AcceptOptions{CompressionMode: websocket.CompressionDisabled}

// writeWSError menolak koneksi WebSocket dengan close code custom supaya
// frontend bisa membedakan sebab kegagalan (auth vs kuota vs helper).
// Kode 4xxx = application-defined per RFC 6455.
func writeWSError(w http.ResponseWriter, r *http.Request, code int, msg string) {
	conn, err := websocket.Accept(w, r, acceptOptions)
	if err != nil {
		return
	}
	defer conn.Close(websocket.StatusCode(code), alasanTutup(msg))
}

// alasanTutup menyiapkan pesan close frame: RFC 6455 membatasi reason
// 123 byte, dan library menolak frame yang melanggarnya — klien lalu hanya
// melihat 1006 tanpa sebab, padahal pesannya berisi penolakan yang jelas
// (id dari query ikut terbawa di beberapa pesan, jadi panjangnya bisa
// melewati batas). Karakter kendali dibuang supaya pesan tetap satu baris
// dan tidak bisa menyisipkan teks palsu di klien.
func alasanTutup(s string) string {
	bersih := strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
	const batas = 120 // byte, aman di bawah 123 dan masih valid UTF-8
	if len(bersih) <= batas {
		return bersih
	}
	var potong []rune
	panjang := 0
	for _, r := range bersih {
		if panjang+len(string(r)) > batas {
			break
		}
		potong = append(potong, r)
		panjang += len(string(r))
	}
	return string(potong)
}

// ---- kuota stream log container ----
//
// Berbeda dari registry terminal (yang menghitung PTY), stream log tidak
// memakai slot itu — tapi tetap harus dibatasi: tiap stream memegang satu
// proses `docker logs -f` milik root selama modal terbuka, dan tanpa batas
// satu sesi sudo bisa membuka ribuan hanya dengan satu skrip.
const kuotaLogMaks = 4

var (
	muKuotaLog    sync.Mutex
	kuotaLogAktif int
)

func ambilSlotLog() bool {
	muKuotaLog.Lock()
	defer muKuotaLog.Unlock()
	if kuotaLogAktif >= kuotaLogMaks {
		return false
	}
	kuotaLogAktif++
	return true
}

func lepasSlotLog() {
	muKuotaLog.Lock()
	if kuotaLogAktif > 0 {
		kuotaLogAktif--
	}
	muKuotaLog.Unlock()
}

func (s *Server) handleWSMetrics(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.sessionFromRequest(r)
	if !ok {
		writeWSError(w, r, 4401, "Sesi tidak valid")
		return
	}
	conn, err := websocket.Accept(w, r, acceptOptions)
	if err != nil {
		return
	}
	defer conn.CloseNow()

	interval := time.Duration(s.store.Preferences(sess.Username).PollingInterval) * time.Millisecond
	id := s.registerWS(interval)
	defer s.unregisterWS(id)
	s.applyFastestInterval()

	ch := s.collector.Subscribe()
	defer s.collector.Unsubscribe(ch)

	ctx := conn.CloseRead(r.Context())
	stopSesi := s.pantauSesi(ctx, sess, func() { _ = conn.Close(4401, "Sesi berakhir") })
	defer stopSesi()

	// Kirim snapshot terakhir langsung supaya UI tidak kosong menunggu tick.
	if first, err := json.Marshal(s.collector.Last()); err == nil {
		_ = conn.Write(ctx, websocket.MessageText, first)
	}

	var lastSent time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case frame := <-ch:
			// Payload sudah di-serialize sekali oleh collector; di sini hanya
			// difilter sesuai interval milik client ini.
			if time.Since(lastSent) < interval-10*time.Millisecond {
				continue
			}
			lastSent = time.Now()
			writeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			err := conn.Write(writeCtx, websocket.MessageText, frame.JSON)
			cancel()
			if err != nil {
				return
			}
		}
	}
}

func (s *Server) registerWS(d time.Duration) int64 {
	s.wsMu.Lock()
	defer s.wsMu.Unlock()
	s.wsNextID++
	id := s.wsNextID
	s.wsIntervals[id] = d
	return id
}

func (s *Server) unregisterWS(id int64) {
	s.wsMu.Lock()
	delete(s.wsIntervals, id)
	s.wsMu.Unlock()
	s.applyFastestInterval()
}

// queryDim membaca dimensi terminal dari query dan menjepitnya ke rentang
// wajar. Tanpa penjepitan, nilai negatif atau sangat besar dibungkus menjadi
// uint16 (mis. -1 → 65535) dan PTY diminta menyiapkan layar selebar itu.
func queryDim(r *http.Request, key string, def, min, max int) uint16 {
	n := queryInt(r, key, def)
	if n < min {
		n = min
	}
	if n > max {
		n = max
	}
	return uint16(n)
}

type terminalClientMsg struct {
	Type string `json:"type"` // "input" | "resize"
	Data string `json:"data,omitempty"`
	Cols uint16 `json:"cols,omitempty"`
	Rows uint16 `json:"rows,omitempty"`
}

// opsiSesiPTY: bagaimana satu sesi PTY di belakang WebSocket dibuka. Dua
// pemakai — terminal host dan terminal container — berbeda di cara stream
// helper dibuka dan di mana sesinya dicatat, bukan di cara sesi PTY-nya
// dijalankan; itulah yang disatukan di wsSesiPTY.
type opsiSesiPTY struct {
	// dial membuka stream helper. Dipanggil SETELAH kuota didapat dan
	// SEBELUM handshake WebSocket, supaya kegagalan helper terbaca klien
	// sebagai close code, bukan upgrade sukses yang langsung mati.
	dial func(cols, rows uint16) (*helperclient.Stream, error)
	// aktivitas/pesan/detail untuk activity log, dicatat sesudah handshake.
	aktivitas string
	pesan     string
	detail    map[string]any
	// adaKanalStop: sesi yang bisa dihapus dari panel (tombol "Hapus sesi"
	// di halaman Terminal) ditutup dengan kode 4409 saat kanal stop ditutup.
	adaKanalStop bool
}

// wsSesiPTY menjalankan satu sesi PTY di balik WebSocket: kuota, handshake,
// pemantauan sesi login, dan jembatan dua arah. Blokir sampai sesi selesai.
func (s *Server) wsSesiPTY(w http.ResponseWriter, r *http.Request, sess store.Session, o opsiSesiPTY) {
	// Kuota dicek SEBELUM PTY di-spawn, dan ditolak dengan pesan jelas —
	// bukan gagal diam-diam atau membiarkan mesin kelebihan beban.
	slot, stop, err := s.terminals.Acquire()
	if err != nil {
		if errors.Is(err, terminal.ErrFull) {
			writeWSError(w, r, 4408, "Kuota sesi terminal penuh")
			return
		}
		writeWSError(w, r, 4500, err.Error())
		return
	}
	release := func() { s.terminals.Release(slot) }
	defer release()

	cols := queryDim(r, "cols", 80, 20, 1000)
	rows := queryDim(r, "rows", 24, 5, 500)

	stream, err := o.dial(cols, rows)
	if err != nil {
		// Lepaskan slot sebelum tulis close code agar tidak bocor.
		release()
		writeWSError(w, r, 4500, err.Error())
		return
	}
	defer stream.Close()

	conn, err := websocket.Accept(w, r, acceptOptions)
	if err != nil {
		return
	}
	defer conn.CloseNow()
	s.store.LogActivity(sess.Username, o.aktivitas, o.pesan, o.detail, clientIP(r))

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	// Sesi tidak sah lagi di tengah jalan (logout di tab lain, password
	// diganti, akun dihapus) → shell ditutup, bukan dibiarkan hidup.
	stopSesi := s.pantauSesi(ctx, sess, func() {
		_ = conn.Close(4401, "Sesi berakhir")
		cancel()
	})
	defer stopSesi()

	// Koneksi helper harus ikut ditutup begitu konteks batal — kalau tidak,
	// pembacaan di alirkanDuplex tetap terblokir dan handler tidak selesai.
	go func() {
		<-ctx.Done()
		_ = stream.Close()
	}()

	// Sesi dihapus dari panel ("Hapus sesi") → kanal stop ditutup. Kode
	// sendiri supaya browser bisa membedakannya dari putus koneksi biasa.
	// Release sendiri juga menutup kanal ini, jadi goroutine-nya selalu
	// selesai saat handler selesai.
	if o.adaKanalStop {
		go func() {
			<-stop
			_ = conn.Close(4409, "Sesi terminal dihapus")
			cancel()
		}()
	}

	alirkanDuplex(ctx, conn, stream)
}

// alirkanDuplex menjembatani stream helper (PTY) dengan WebSocket: keluaran
// helper menjadi pesan biner, frame klien (TermFrameData/TermFrameResize)
// diteruskan ke helper. Blokir sampai salah satu sisi mati.
func alirkanDuplex(ctx context.Context, conn *websocket.Conn, stream io.ReadWriteCloser) {
	// PTY → browser.
	go func() {
		buf := make([]byte, 8192)
		for {
			n, err := stream.Read(buf)
			if n > 0 {
				if wErr := conn.Write(ctx, websocket.MessageBinary, buf[:n]); wErr != nil {
					return
				}
			}
			if err != nil {
				if !errors.Is(err, io.EOF) {
					log.Printf("terminal: baca PTY: %v", err)
				}
				_ = conn.Close(websocket.StatusNormalClosure, "PTY closed")
				return
			}
		}
	}()

	// browser → PTY, dibungkus frame supaya input dan resize lewat satu kanal.
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		var msg terminalClientMsg
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}
		switch msg.Type {
		case "input":
			if err := writeTermFrame(stream, helperproto.TermFrameData, []byte(msg.Data)); err != nil {
				return
			}
		case "resize":
			payload := make([]byte, 4)
			binary.BigEndian.PutUint16(payload[0:2], msg.Cols)
			binary.BigEndian.PutUint16(payload[2:4], msg.Rows)
			if err := writeTermFrame(stream, helperproto.TermFrameResize, payload); err != nil {
				return
			}
		}
	}
}

func (s *Server) handleWSTerminal(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.sessionFromRequest(r)
	if !ok {
		// 4401 = sesi invalid (bukan HTTP — websocket.Status code custom).
		writeWSError(w, r, 4401, "Sesi tidak valid")
		return
	}

	cmdParam := r.URL.Query().Get("cmd")

	// Allowlist command yang boleh dieksekusi langsung untuk keamanan
	allowedCmds := map[string]string{
		"hermes":      "hermes",
		"claude-code": "claude",
		"claude":      "claude",
		"codex":       "codex",
		"opencode":    "opencode",
		"openclaw":    "openclaw",
	}
	var execCmd string
	if cmdParam != "" {
		if c, ok := allowedCmds[cmdParam]; ok {
			execCmd = c
		}
	}

	s.wsSesiPTY(w, r, sess, opsiSesiPTY{
		dial: func(cols, rows uint16) (*helperclient.Stream, error) {
			return s.helper.Stream(helperproto.CmdTerminalStart, sess.HelperToken,
				helperproto.TerminalArgs{Cols: cols, Rows: rows, Command: execCmd})
		},
		aktivitas:    "terminal_open",
		pesan:        "buka sesi terminal",
		adaKanalStop: true,
	})
}

// handleWSDockerTerminal membuka sesi shell DI DALAM container
// (`docker exec -i -t <id> <shell>`) lewat WebSocket — backend untuk tombol
// Terminal di halaman Docker. Sudo dicek di sini karena browser tidak bisa
// memasang header Authorization pada WebSocket; cookie sessionlah yang
// terkirim, dan helper menegakkan sudo-nya sekali lagi per permintaan.
func (s *Server) handleWSDockerTerminal(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.sessionFromRequest(r)
	if !ok {
		writeWSError(w, r, 4401, "Sesi tidak valid")
		return
	}
	s.segarkanSudo(&sess)
	if !sess.Sudo {
		writeWSError(w, r, 4403, "Aksi ini butuh akses sudo")
		return
	}
	id := r.URL.Query().Get("id")
	if id == "" {
		writeWSError(w, r, 4400, "id container kosong")
		return
	}

	s.wsSesiPTY(w, r, sess, opsiSesiPTY{
		dial: func(cols, rows uint16) (*helperclient.Stream, error) {
			return s.helper.Stream(helperproto.CmdDockerTerm, sess.HelperToken,
				helperproto.DockerTermArgs{ID: id, Cols: cols, Rows: rows})
		},
		aktivitas: "docker_term_open",
		pesan:     "buka terminal container",
		detail:    map[string]any{"container": id},
	})
}

func writeTermFrame(w io.Writer, kind byte, payload []byte) error {
	header := make([]byte, 5)
	header[0] = kind
	binary.BigEndian.PutUint32(header[1:], uint32(len(payload)))
	if _, err := w.Write(header); err != nil {
		return err
	}
	_, err := w.Write(payload)
	return err
}

// handleWSDockerLogs menstream log container ke browser selama modal log
// terbuka. Berbeda dari GET /api/docker/containers/{id}/logs yang hanya
// mengambil satu potongan pada saat dipanggil, jalur ini mempertahankan
// `docker logs -f` di helper sehingga baris baru sampai ke layar begitu
// container menulisnya.
//
// Sesi dicek di sini, bukan lewat middleware grup /api: browser tidak bisa
// memasang header Authorization pada WebSocket, jadi cookie session yang
// dipakai — dan status sudo tetap diperiksa sebelum stream dibuka.
func (s *Server) handleWSDockerLogs(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.sessionFromRequest(r)
	if !ok {
		writeWSError(w, r, 4401, "Sesi tidak valid")
		return
	}
	s.segarkanSudo(&sess)
	if !sess.Sudo {
		writeWSError(w, r, 4403, "Aksi ini butuh akses sudo")
		return
	}
	id := r.URL.Query().Get("id")
	if id == "" {
		writeWSError(w, r, 4400, "id container kosong")
		return
	}
	tail := queryInt(r, "tail", 200)
	if tail < 1 {
		tail = 1
	}
	if tail > maxTailLog {
		tail = maxTailLog
	}

	// Kuota SEBELUM dial: tanpa batas, satu sesi sudo bisa membuka stream
	// tanpa henti — tiap stream memegang satu proses `docker logs -f` milik
	// root selama modal terbuka. Satu halaman Docker hanya punya satu modal
	// log, jadi 4 berarti empat tab/sesi sekaligus.
	if !ambilSlotLog() {
		writeWSError(w, r, 4408, "Terlalu banyak stream log container terbuka — tutup salah satu dulu")
		return
	}
	defer lepasSlotLog()

	stream, err := s.helper.Stream(helperproto.CmdDockerLogs, sess.HelperToken,
		helperproto.DockerLogsArgs{ID: id, Tail: tail})
	if err != nil {
		writeWSError(w, r, 4500, err.Error())
		return
	}
	defer stream.Close()

	conn, err := websocket.Accept(w, r, acceptOptions)
	if err != nil {
		return
	}
	defer conn.CloseNow()
	s.store.LogActivity(sess.Username, "docker_logs_open", "buka log container (streaming)",
		map[string]any{"container": id}, clientIP(r))

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	stopSesi := s.pantauSesi(ctx, sess, func() {
		_ = conn.Close(4401, "Sesi berakhir")
		cancel()
	})
	defer stopSesi()

	// Koneksi helper harus ikut ditutup begitu konteks batal — kalau tidak,
	// pembacaan di bawah tetap terblokir menunggu baris log berikutnya dan
	// handler tidak pernah selesai.
	go func() {
		<-ctx.Done()
		_ = stream.Close()
	}()

	// Browser tidak pernah mengirim apa pun di jalur ini; koneksi dibaca
	// hanya untuk melihat kapan browser menutupnya.
	go func() {
		defer cancel()
		for {
			if _, _, err := conn.Read(ctx); err != nil {
				return
			}
		}
	}()

	// helper → browser. Potongan byte diteruskan mentah sebagai PESAN
	// BINER: log container adalah byte sembarangan, dan memotongnya di
	// batas 8192 bisa membelah karakter UTF-8 multi-byte — frame teks yang
	// tidak valid membuat browser menutup koneksi (1007) dan streaming
	// mati diam-diam. Penerima menyambung byte mentah (lihat docker.tsx).
	buf := make([]byte, 8192)
	for {
		n, err := stream.Read(buf)
		if n > 0 {
			if wErr := conn.Write(ctx, websocket.MessageBinary, buf[:n]); wErr != nil {
				return
			}
		}
		if err != nil {
			_ = conn.Close(websocket.StatusNormalClosure, "log selesai")
			return
		}
	}
}
