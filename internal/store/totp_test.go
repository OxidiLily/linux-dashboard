package store

import (
	"bytes"
	"path/filepath"
	"testing"
)

func TestTOTPMenyimpanSecretTerenkripsiDanCounterReplayAtomik(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "totp.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	ciphertext := []byte("nonce-dan-ciphertext-bukan-secret")
	recovery := [][]byte{[]byte("hash-satu"), []byte("hash-dua")}
	if err := st.SetTOTPPending("ani", ciphertext); err != nil {
		t.Fatal(err)
	}
	pending, ok, err := st.TOTPPending("ani")
	if err != nil || !ok || !bytes.Equal(pending, ciphertext) {
		t.Fatalf("pending=%q ok=%v err=%v", pending, ok, err)
	}
	if err := st.EnableTOTP("ani", recovery); err != nil {
		t.Fatal(err)
	}
	status, err := st.TOTPStatus("ani")
	if err != nil || !status.Enabled || status.RecoveryRemaining != 2 {
		t.Fatalf("status=%+v err=%v", status, err)
	}

	accepted, err := st.AdvanceTOTPCounter("ani", 123)
	if err != nil || !accepted {
		t.Fatalf("counter pertama accepted=%v err=%v", accepted, err)
	}
	accepted, err = st.AdvanceTOTPCounter("ani", 123)
	if err != nil || accepted {
		t.Fatalf("replay accepted=%v err=%v", accepted, err)
	}
	accepted, err = st.AdvanceTOTPCounter("ani", 124)
	if err != nil || !accepted {
		t.Fatalf("counter baru accepted=%v err=%v", accepted, err)
	}
}

func TestDeleteUserSecurityStateMenghapusTOTPDanRecovery(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "totp-delete.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.SetTOTPPending("ani", []byte("ciphertext")); err != nil {
		t.Fatal(err)
	}
	if err := st.EnableTOTP("ani", [][]byte{[]byte("hash")}); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteUserSecurityState("ani"); err != nil {
		t.Fatal(err)
	}
	status, err := st.TOTPStatus("ani")
	if err != nil || status.Enabled || status.RecoveryRemaining != 0 {
		t.Fatalf("state TOTP tersisa sesudah user dihapus: %+v err=%v", status, err)
	}
}

func TestTOTPRecoverySekaliPakaiDanDisableMenghapusData(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "totp.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.SetTOTPPending("ani", []byte("ciphertext")); err != nil {
		t.Fatal(err)
	}
	if err := st.EnableTOTP("ani", [][]byte{[]byte("hash")}); err != nil {
		t.Fatal(err)
	}
	used, err := st.ConsumeTOTPRecovery("ani", []byte("hash"))
	if err != nil || !used {
		t.Fatalf("consume pertama=%v err=%v", used, err)
	}
	used, err = st.ConsumeTOTPRecovery("ani", []byte("hash"))
	if err != nil || used {
		t.Fatalf("replay recovery=%v err=%v", used, err)
	}
	if err := st.DisableTOTP("ani"); err != nil {
		t.Fatal(err)
	}
	status, err := st.TOTPStatus("ani")
	if err != nil || status.Enabled {
		t.Fatalf("status sesudah disable=%+v err=%v", status, err)
	}
	if _, ok, err := st.TOTPSecret("ani"); err != nil || ok {
		t.Fatalf("secret masih ada ok=%v err=%v", ok, err)
	}
}
