package totp

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

const DefaultKeyPath = "/var/lib/linux-dashboard-helper/totp.key"

type Cipher struct{ aead cipher.AEAD }

func KeyPath() string {
	if p := os.Getenv("DASHBOARD_TOTP_KEY"); p != "" {
		return p
	}
	return DefaultKeyPath
}

func LoadCipher(path string) (*Cipher, error) {
	// Buka sekali tanpa mengikuti symlink, lalu validasi dan baca dari descriptor
	// yang sama. Lstat(path) kemudian ReadFile(path) memberi celah symlink-swap.
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, fmt.Errorf("buka key TOTP: %w", err)
	}
	f := os.NewFile(uintptr(fd), path)
	if f == nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("descriptor key TOTP tidak valid")
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat key TOTP: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("key TOTP harus regular file, bukan symlink/perangkat")
	}
	// Installer membuat root:<service-user> 0640: proses web perlu group-read,
	// tetapi group-write/execute dan seluruh akses other harus ditolak.
	if info.Mode().Perm()&0o027 != 0 {
		return nil, fmt.Errorf("permission key TOTP terlalu terbuka: maksimal 0640")
	}
	key := make([]byte, 33)
	n, readErr := f.Read(key)
	if readErr != nil && n == 0 {
		return nil, fmt.Errorf("baca key TOTP: %w", readErr)
	}
	if n != 32 {
		return nil, fmt.Errorf("key TOTP harus tepat 32 byte")
	}
	block, err := aes.NewCipher(key[:32])
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Cipher{aead: aead}, nil
}

func (c *Cipher) EncryptFor(username string, plain []byte) ([]byte, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return c.aead.Seal(nonce, nonce, plain, []byte(username)), nil
}

func (c *Cipher) DecryptFor(username string, sealed []byte) ([]byte, error) {
	n := c.aead.NonceSize()
	if len(sealed) < n {
		return nil, fmt.Errorf("ciphertext TOTP rusak")
	}
	return c.aead.Open(nil, sealed[:n], sealed[n:], []byte(username))
}

// Encrypt/Decrypt dipertahankan untuk pemanggil non-akun; penyimpanan secret
// user wajib memakai varian For agar ciphertext terikat ke username.
func (c *Cipher) Encrypt(plain []byte) ([]byte, error)  { return c.EncryptFor("", plain) }
func (c *Cipher) Decrypt(sealed []byte) ([]byte, error) { return c.DecryptFor("", sealed) }

func GenerateSecret() (string, error) {
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b), nil
}

func URI(username, secret string) string {
	label := "Linux Dashboard:" + username
	q := url.Values{"secret": {secret}, "issuer": {"Linux Dashboard"}, "algorithm": {"SHA1"}, "digits": {"6"}, "period": {"30"}}
	return "otpauth://totp/" + url.PathEscape(label) + "?" + q.Encode()
}

func Code(secret string, counter int64) (string, error) {
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(strings.TrimSpace(secret)))
	if err != nil {
		return "", err
	}
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(counter))
	mac := hmac.New(sha1.New, key)
	_, _ = mac.Write(msg[:])
	sum := mac.Sum(nil)
	off := sum[len(sum)-1] & 15
	v := (uint32(sum[off])&0x7f)<<24 | uint32(sum[off+1])<<16 | uint32(sum[off+2])<<8 | uint32(sum[off+3])
	return fmt.Sprintf("%06d", v%1_000_000), nil
}

func Verify(secret, code string, now time.Time) (int64, bool) {
	if len(code) != 6 {
		return 0, false
	}
	base := now.Unix() / 30
	for _, counter := range []int64{base, base - 1, base + 1} {
		expected, err := Code(secret, counter)
		if err == nil && hmac.Equal([]byte(expected), []byte(code)) {
			return counter, true
		}
	}
	return 0, false
}

func normalizeRecovery(code string) string {
	code = strings.ToUpper(code)
	return strings.Map(func(r rune) rune {
		if r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, code)
}

func RecoveryHash(code string) []byte {
	sum := sha256.Sum256([]byte(normalizeRecovery(code)))
	return sum[:]
}

func GenerateRecovery(n int) ([]string, [][]byte, error) {
	codes := make([]string, n)
	hashes := make([][]byte, n)
	alphabet := "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	for i := range codes {
		b := make([]byte, 10)
		if _, err := rand.Read(b); err != nil {
			return nil, nil, err
		}
		for j := range b {
			b[j] = alphabet[int(b[j])%len(alphabet)]
		}
		codes[i] = string(b[:5]) + "-" + string(b[5:])
		hashes[i] = RecoveryHash(codes[i])
	}
	return codes, hashes, nil
}
