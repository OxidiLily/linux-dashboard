package api

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"linux-dashboard/OxidiLily/internal/config"
	"linux-dashboard/OxidiLily/internal/store"
)

// Handshake WebSocket hanya memeriksa sesi satu kali. Tanpa pemantauan, cookie
// yang dicuri tetap memberi terminal shell yang hidup walaupun korban sudah
// logout atau mengganti password — dan terminal adalah hal paling berharga yang
// bisa didapat penyerang dari sesi itu.
func TestPantauSesiMenutupKoneksiSaatSesiDicabut(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("buka store: %v", err)
	}
	defer st.Close()

	srv := New(config.Config{}, st, nil, nil, nil)
	sess, err := st.CreateSession("alice", "/home/alice", "192.0.2.1", false, "tok-alice", time.Hour)
	if err != nil {
		t.Fatalf("buat sesi: %v", err)
	}

	asli := sesiPeriksa
	sesiPeriksa = 10 * time.Millisecond
	defer func() { sesiPeriksa = asli }()

	ditutup := make(chan struct{})
	stop := srv.pantauSesi(context.Background(), sess, func() { close(ditutup) })
	defer stop()

	// Sesi masih sah: koneksi tidak boleh ditutup.
	select {
	case <-ditutup:
		t.Fatal("koneksi ditutup padahal sesi masih sah")
	case <-time.After(60 * time.Millisecond):
	}

	if err := st.DeleteSession(sess.ID); err != nil {
		t.Fatalf("cabut sesi: %v", err)
	}
	select {
	case <-ditutup:
	case <-time.After(2 * time.Second):
		t.Fatal("koneksi tidak ditutup setelah sesi dicabut")
	}
}

// stop() dipanggil lewat defer pada handler; memanggilnya dua kali tidak boleh
// panik (close pada channel tertutup).
func TestPantauSesiStopIdempoten(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("buka store: %v", err)
	}
	defer st.Close()

	srv := New(config.Config{}, st, nil, nil, nil)
	sess, _ := st.CreateSession("alice", "/home/alice", "192.0.2.1", false, "tok-alice", time.Hour)

	stop := srv.pantauSesi(context.Background(), sess, func() {})
	stop()
	stop()
}
