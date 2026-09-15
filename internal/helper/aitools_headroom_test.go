package helper

import "testing"

// unitHeadroomPerluGanti hanya benar pada unit bawaan panel yang belum punya
// jembatan pid file (ExecStartPost). Unit kustom admin tidak boleh ditimpa.
func TestUnitHeadroomPerluGanti(t *testing.T) {
	isi := string(unitHeadroomTertanam)
	if unitHeadroomPerluGanti(isi) {
		t.Fatalf("unit tertanam (sudah ber-ExecStartPost) tidak boleh dianggap perlu ganti")
	}
	lama := "[Service]\nExecStart=/opt/headroom/bin/headroom proxy --host 127.0.0.1 --port 8787\nRestart=on-failure\n"
	if !unitHeadroomPerluGanti(lama) {
		t.Fatalf("unit lama tanpa ExecStartPost harus dianggap perlu ganti")
	}
	kustom := "[Service]\nExecStart=/opt/headroom/bin/headroom proxy --host 127.0.0.1 --port 9999\n"
	if unitHeadroomPerluGanti(kustom) {
		t.Fatalf("unit kustom (port lain) tetap milik admin — tidak boleh ditimpa")
	}
}
