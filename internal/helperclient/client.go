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
	"log"
	"net"
	"os"
	"time"

	"linux-dashboard/OxidiLily/internal/helperproto"
)

type Client struct {
	socket string
	secret []byte
}

// batasFrameResp membatasi satu baris response helper. Request sudah dibatasi
// 2 MiB di sisi helper, response tidak — padahal isinya keluaran command yang
// berjalan sebagai root (metadata docker, daftar paket, lpstat) dan panjangnya
// ditentukan mesin, bukan oleh pemanggil. Tanpa batas, satu command yang
// keluarannya membengkak cukup untuk menghabiskan memori proses web.
const batasFrameResp = 8 << 20

// bacaBarisResp membaca satu baris response dengan batas ukuran. ReadBytes
// akan mengalokasikan sebesar apa pun sampai ketemu newline, jadi pemotongan
// harus terjadi saat membaca, bukan sesudahnya.
func bacaBarisResp(br *bufio.Reader) ([]byte, error) {
	var buf []byte
	for {
		chunk, err := br.ReadSlice('\n')
		buf = append(buf, chunk...)
		if len(buf) > batasFrameResp {
			return nil, fmt.Errorf("response helper melebihi %d byte", batasFrameResp)
		}
		if err == nil {
			return buf, nil
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		return nil, err
	}
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

// New menyiapkan client helper dari berkas secret yang dibaca web app.
//
// legacySecretPath adalah lokasi secret versi lama (satu direktori dengan state
// dir web). Ia dibaca hanya kalau secretPath belum ada. Helper daemon punya
// fallback yang sama, dan keduanya harus sepakat: kalau salah satu sisi menolak
// jalan sementara sisi lain jalan, pembaruan panel berakhir dengan web app yang
// tidak bisa menghubungi helper sama sekali. Migrasi tetap disarankan dan
// diperingatkan lewat log — selama secret ada di state dir web, user service web
// bisa menggantinya.
func New(socketPath, secretPath, legacySecretPath string) (*Client, error) {
	secret, err := os.ReadFile(secretPath)
	if err != nil {
		if legacySecretPath != "" && legacySecretPath != secretPath {
			if b, legacyErr := os.ReadFile(legacySecretPath); legacyErr == nil {
				log.Printf("peringatan: secret helper dibaca dari lokasi lama %s — pindahkan ke %s "+
					"(lalu hapus yang lama): selama ada di sana, user service web bisa menggantinya",
					legacySecretPath, secretPath)
				return &Client{socket: socketPath, secret: trimNewline(b)}, nil
			}
		}
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
//
// token adalah capability yang diterbitkan helper saat login. Ia ikut di
// SETIAP permintaan karena helper tidak lagi mempercayai klaim identitas dari
// pemanggil: satu-satunya hal yang menentukan user mana yang dijalani command
// ini adalah token tersebut. token kosong selalu ditolak helper.
//
// Mengembalikan koneksi + reader yang sudah membaca baris response, supaya
// pemanggil bisa melanjutkan sebagai stream kalau perlu.
func (c *Client) dial(cmd, token string, args any) (net.Conn, *bufio.Reader, *helperproto.Response, error) {
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
		Cmd:   cmd,
		Token: token,
		Args:  raw,
		TS:    time.Now().Unix(),
		Nonce: hex.EncodeToString(nonce),
	}
	payload, err := json.Marshal(req)
	if err != nil {
		return nil, nil, nil, err
	}

	conn, err := net.DialTimeout("unix", c.socket, 5*time.Second)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("helper daemon tidak dapat dihubungi: %w", err)
	}
	if cmd == helperproto.CmdCronGet || cmd == helperproto.CmdCronPut {
		_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	}
	line := append([]byte(helperproto.Sign(c.secret, payload)+" "), payload...)
	line = append(line, '\n')
	if _, err := conn.Write(line); err != nil {
		conn.Close()
		return nil, nil, nil, err
	}

	br := bufio.NewReader(conn)
	respLine, err := bacaBarisResp(br)
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
	_ = conn.SetDeadline(time.Time{})
	return conn, br, &resp, nil
}

// Call menjalankan command non-stream dan mengurai hasilnya ke out.
//
// token adalah capability sesi dari helper (LoginResult.Token). Ia wajib
// dikirim untuk semua command kecuali auth.login; helper menolak permintaan
// dengan token kosong/tidak dikenal/kedaluwarsa sebagai session_invalid.
func (c *Client) Call(cmd, token string, args any, out any) error {
	conn, _, resp, err := c.dial(cmd, token, args)
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

func (c *Client) Stream(cmd, token string, args any) (*Stream, error) {
	conn, br, resp, err := c.dial(cmd, token, args)
	if err != nil {
		return nil, err
	}
	return &Stream{Conn: conn, R: br, Resp: resp}, nil
}
