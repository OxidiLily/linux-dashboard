package api

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/coder/websocket"

	"linux-dashboard/OxidiLily/internal/helperproto"
	"linux-dashboard/OxidiLily/internal/terminal"
)

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
	defer conn.Close(websocket.StatusCode(code), msg)
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

type terminalClientMsg struct {
	Type string `json:"type"` // "input" | "resize"
	Data string `json:"data,omitempty"`
	Cols uint16 `json:"cols,omitempty"`
	Rows uint16 `json:"rows,omitempty"`
}

func (s *Server) handleWSTerminal(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.sessionFromRequest(r)
	if !ok {
		// 4401 = sesi invalid (bukan HTTP — websocket.Status code custom).
		writeWSError(w, r, 4401, "Sesi tidak valid")
		return
	}
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

	cols := uint16(queryInt(r, "cols", 80))
	rows := uint16(queryInt(r, "rows", 24))
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

	stream, err := s.helper.Stream(helperproto.CmdTerminalStart, sess.Username,
		helperproto.TerminalArgs{Cols: cols, Rows: rows, Command: execCmd})
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
	s.store.LogActivity(sess.Username, "terminal_open", "buka sesi terminal", nil, clientIP(r))

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	// Sesi dihapus dari panel ("Hapus sesi") → kanal stop ditutup. Koneksi
	// ditutup dengan kode sendiri supaya browser bisa membedakannya dari
	// putus koneksi biasa. Release sendiri juga menutup kanal ini, jadi
	// goroutine-nya selalu selesai saat handler selesai.
	go func() {
		<-stop
		_ = conn.Close(4409, "Sesi terminal dihapus")
		cancel()
	}()

	// PTY → browser.
	go func() {
		defer cancel()
		buf := make([]byte, 8192)
		for {
			n, err := stream.Read(buf)
			if n > 0 {
				if err := conn.Write(ctx, websocket.MessageBinary, buf[:n]); err != nil {
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
