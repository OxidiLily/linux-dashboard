// Package helperclient adalah client untuk helper daemon.
// Web app tidak pernah eksekusi command privileged sendiri — semuanya lewat
// sini, dan setiap request ditandatangani HMAC.
package helperclient

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"time"

	"linux-dashboard/OxidiLily/internal/helperproto"
)

type Client struct {
	socket string
	secret []byte
}

// Error membawa kode terstruktur dari helper daemon supaya handler API bisa
// memetakannya ke HTTP status yang tepat (mis. requires_sudo → 403).
type Error struct {
	Code string
	Msg  string
	// Params mengisi placeholder pada kalimat yang disusun frontend.
	Params []string
}

func (e *Error) Error() string { return e.Msg }

// Params mengembalikan parameter kalimat error, kosong bila bukan error helper.
func Params(err error) []string {
	var he *Error
	if errors.As(err, &he) {
		return he.Params
	}
	return nil
}

func Code(err error) string {
	var he *Error
	if errors.As(err, &he) {
		return he.Code
	}
	return ""
}

func New(socketPath, secretPath string) (*Client, error) {
	secret, err := os.ReadFile(secretPath)
	if err != nil {
		return nil, fmt.Errorf("baca secret helper: %w", err)
	}
	return &Client{socket: socketPath, secret: trimNewline(secret)}, nil
}

func trimNewline(b []byte) []byte {
	for len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == '\r') {
		b = b[:len(b)-1]
	}
	return b
}

// dial membuka koneksi dan mengirim request bertanda tangan.
// Mengembalikan koneksi + reader yang sudah membaca baris response, supaya
// pemanggil bisa melanjutkan sebagai stream kalau perlu.
func (c *Client) dial(cmd, username string, args any) (net.Conn, *bufio.Reader, *helperproto.Response, error) {
	var raw json.RawMessage
	if args != nil {
		b, err := json.Marshal(args)
		if err != nil {
			return nil, nil, nil, err
		}
		raw = b
	}
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return nil, nil, nil, err
	}
	req := helperproto.Request{
		Cmd:      cmd,
		Username: username,
		Args:     raw,
		TS:       time.Now().Unix(),
		Nonce:    hex.EncodeToString(nonce),
	}
	payload, err := json.Marshal(req)
	if err != nil {
		return nil, nil, nil, err
	}

	conn, err := net.DialTimeout("unix", c.socket, 5*time.Second)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("helper daemon tidak dapat dihubungi: %w", err)
	}
	line := append([]byte(helperproto.Sign(c.secret, payload)+" "), payload...)
	line = append(line, '\n')
	if _, err := conn.Write(line); err != nil {
		conn.Close()
		return nil, nil, nil, err
	}

	br := bufio.NewReader(conn)
	respLine, err := br.ReadBytes('\n')
	if err != nil {
		conn.Close()
		return nil, nil, nil, fmt.Errorf("baca response helper: %w", err)
	}
	var resp helperproto.Response
	if err := json.Unmarshal(respLine, &resp); err != nil {
		conn.Close()
		return nil, nil, nil, fmt.Errorf("response helper tidak valid: %w", err)
	}
	if !resp.OK {
		conn.Close()
		return nil, nil, nil, &Error{Code: resp.Code, Msg: resp.Error, Params: resp.Params}
	}
	return conn, br, &resp, nil
}

// Call menjalankan command non-stream dan mengurai hasilnya ke out.
func (c *Client) Call(cmd, username string, args any, out any) error {
	conn, _, resp, err := c.dial(cmd, username, args)
	if err != nil {
		return err
	}
	defer conn.Close()
	if out != nil && len(resp.Data) > 0 {
		return json.Unmarshal(resp.Data, out)
	}
	return nil
}

// Stream membuka command bertipe stream. Pemanggil bertanggung jawab menutup
// koneksi. Reader dikembalikan terpisah karena sebagian byte stream bisa
// sudah ter-buffer saat membaca baris response.
type Stream struct {
	net.Conn
	R *bufio.Reader
	// Resp adalah response awal helper — untuk file.read isinya metadata file.
	Resp *helperproto.Response
}

func (s *Stream) Read(p []byte) (int, error) { return s.R.Read(p) }

// CloseWrite menutup arah tulis supaya helper melihat EOF dan menyelesaikan
// penulisan file. Tanpa ini, upload akan menggantung menunggu data lanjutan.
func (s *Stream) CloseWrite() error {
	if uc, ok := s.Conn.(*net.UnixConn); ok {
		return uc.CloseWrite()
	}
	return nil
}

// Selesai menutup arah tulis lalu MENUNGGU konfirmasi helper bahwa byte-nya
// benar-benar mendarat di disk. Response awal helper dikirim sebelum file
// dibuka — ia hanya menandakan stream siap menerima data — jadi tanpa
// menunggu konfirmasi akhir, kegagalan menulis (izin ditolak, disk penuh)
// tidak terlihat sama sekali: file kecil muat di buffer socket sehingga
// penyalinan tetap sukses, dan panel melaporkan "berhasil" untuk berkas yang
// tidak pernah ada. Semua penulis file WAJIB memakai ini, bukan CloseWrite.
func (s *Stream) Selesai() error {
	if err := s.CloseWrite(); err != nil {
		return err
	}
	line, err := s.R.ReadBytes('\n')
	if err != nil {
		return fmt.Errorf("helper terputus sebelum penulisan selesai: %w", err)
	}
	var resp helperproto.Response
	if err := json.Unmarshal(trimNewline(line), &resp); err != nil {
		return fmt.Errorf("konfirmasi helper tidak valid: %w", err)
	}
	if !resp.OK {
		return &Error{Code: resp.Code, Msg: resp.Error, Params: resp.Params}
	}
	return nil
}

func (c *Client) Stream(cmd, username string, args any) (*Stream, error) {
	conn, br, resp, err := c.dial(cmd, username, args)
	if err != nil {
		return nil, err
	}
	return &Stream{Conn: conn, R: br, Resp: resp}, nil
}

