package store

import (
	"path/filepath"
	"testing"
	"time"
)

func bukaStoreUji(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("buka store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// Password yang direset/diganti tidak ada artinya kalau cookie lama tetap
// berlaku: penyerang yang sudah memegang sesi cukup menunggu sampai TTL habis.
func TestDeleteSessionsByUsernameMencabutSemuaSesiUserItu(t *testing.T) {
	s := bukaStoreUji(t)
	a1, err := s.CreateSession("alice", "/home/alice", "192.0.2.1", false, time.Hour)
	if err != nil {
		t.Fatalf("sesi alice 1: %v", err)
	}
	a2, err := s.CreateSession("alice", "/home/alice", "192.0.2.2", true, time.Hour)
	if err != nil {
		t.Fatalf("sesi alice 2: %v", err)
	}
	b, err := s.CreateSession("bob", "/home/bob", "192.0.2.3", false, time.Hour)
	if err != nil {
		t.Fatalf("sesi bob: %v", err)
	}

	if err := s.DeleteSessionsByUsername("alice"); err != nil {
		t.Fatalf("cabut sesi alice: %v", err)
	}
	if _, ok := s.GetSession(a1.ID); ok {
		t.Error("sesi alice 1 masih hidup")
	}
	if _, ok := s.GetSession(a2.ID); ok {
		t.Error("sesi alice 2 masih hidup")
	}
	if _, ok := s.GetSession(b.ID); !ok {
		t.Error("sesi bob ikut tercabut")
	}
}

// Ganti password sendiri: sesi yang sedang dipakai harus tetap hidup, sesi di
// perangkat lain mati.
func TestDeleteSessionsExceptMenyisakanSesiBerjalan(t *testing.T) {
	s := bukaStoreUji(t)
	ini, err := s.CreateSession("alice", "/home/alice", "192.0.2.1", false, time.Hour)
	if err != nil {
		t.Fatalf("sesi berjalan: %v", err)
	}
	lain, err := s.CreateSession("alice", "/home/alice", "192.0.2.9", false, time.Hour)
	if err != nil {
		t.Fatalf("sesi lain: %v", err)
	}

	if err := s.DeleteSessionsExcept("alice", ini.ID); err != nil {
		t.Fatalf("cabut sesi lain: %v", err)
	}
	if _, ok := s.GetSession(ini.ID); !ok {
		t.Error("sesi yang sedang dipakai ikut tercabut")
	}
	if _, ok := s.GetSession(lain.ID); ok {
		t.Error("sesi di perangkat lain masih hidup")
	}
}
