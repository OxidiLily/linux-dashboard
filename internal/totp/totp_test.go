package totp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCipherAESGCMRoundTripDanMenolakKeySalah(t *testing.T) {
	keyPath := filepath.Join(t.TempDir(), "totp.key")
	if err := os.WriteFile(keyPath, []byte(strings.Repeat("k", 32)), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := LoadCipher(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := c.Encrypt([]byte("JBSWY3DPEHPK3PXP"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(sealed), "JBSWY3DPEHPK3PXP") {
		t.Fatal("secret tersimpan plaintext")
	}
	plain, err := c.Decrypt(sealed)
	if err != nil || string(plain) != "JBSWY3DPEHPK3PXP" {
		t.Fatalf("plain=%q err=%v", plain, err)
	}
	bound, err := c.EncryptFor("ani", []byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.DecryptFor("budi", bound); err == nil {
		t.Fatal("ciphertext dapat dipindah ke akun lain")
	}

	otherPath := filepath.Join(t.TempDir(), "other.key")
	if err := os.WriteFile(otherPath, []byte(strings.Repeat("x", 32)), 0o600); err != nil {
		t.Fatal(err)
	}
	other, _ := LoadCipher(otherPath)
	if _, err := other.Decrypt(sealed); err == nil {
		t.Fatal("ciphertext terbuka dengan key salah")
	}
}

func TestLoadCipherWajibKey32Byte(t *testing.T) {
	p := filepath.Join(t.TempDir(), "key")
	if err := os.WriteFile(p, []byte("pendek"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadCipher(p); err == nil {
		t.Fatal("key pendek diterima")
	}
}

func TestLoadCipherMenolakSymlinkDanPermissionTerbuka(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte(strings.Repeat("k", 32)), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadCipher(link); err == nil {
		t.Fatal("symlink diterima")
	}
	if err := os.Chmod(target, 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadCipher(target); err != nil {
		t.Fatalf("0640 installer ditolak: %v", err)
	}
	if err := os.Chmod(target, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadCipher(target); err == nil {
		t.Fatal("permission other diterima")
	}
}

func TestKodeTOTPValidMengembalikanCounter(t *testing.T) {
	secret := "JBSWY3DPEHPK3PXP"
	now := time.Unix(1_700_000_000, 0)
	counter := now.Unix() / 30
	code, err := Code(secret, counter)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := Verify(secret, code, now)
	if !ok || got != counter {
		t.Fatalf("counter=%d ok=%v ingin=%d", got, ok, counter)
	}
	if _, ok := Verify(secret, "000000", now); ok && code != "000000" {
		t.Fatal("kode salah diterima")
	}
}

func TestRecoveryCodeDinormalisasiDanDihash(t *testing.T) {
	a := RecoveryHash("ABCD-EFGH")
	b := RecoveryHash("abcd efgh")
	if string(a) != string(b) {
		t.Fatal("normalisasi recovery tidak konsisten")
	}
	codes, hashes, err := GenerateRecovery(8)
	if err != nil || len(codes) != 8 || len(hashes) != 8 {
		t.Fatalf("codes=%d hashes=%d err=%v", len(codes), len(hashes), err)
	}
}
