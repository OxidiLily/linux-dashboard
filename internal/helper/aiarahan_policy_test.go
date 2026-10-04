package helper

import (
	"strings"
	"testing"
)

func TestArahanPolicySemuaAgent(t *testing.T) {
	for _, agent := range []string{"claude", "codex", "hermes", "opencode", "openclaw"} {
		if len(berkasArahanAgent[agent]) == 0 {
			t.Fatalf("adapter hilang: %s", agent)
		}
		isi := arahanPolicy("/home/test", agent)
		for _, bagian := range []string{"/home/test/DATA/AppData/linux-dashboard/SOUL.md", "grounded-search.py search", "jangan menciptakan hasil", "grounded-search.py record", "konfirmasi eksplisit", "Agent: " + agent} {
			if !strings.Contains(isi, bagian) {
				t.Errorf("%s kehilangan %q", agent, bagian)
			}
		}
	}
}

func TestBlokPolicyTidakMenggantiArahanLain(t *testing.T) {
	awal := "milik user\n\n" + penandaMulai + "\nalat\n" + penandaSelesai + "\n"
	blok := penandaPolicyMulai + "\npolicy\n" + penandaPolicySelesai + "\n"
	isi := gantiBlokDenganPenanda(awal, blok, penandaPolicyMulai, penandaPolicySelesai)
	isi = gantiBlokDenganPenanda(isi, blok, penandaPolicyMulai, penandaPolicySelesai)
	if strings.Count(isi, penandaPolicyMulai) != 1 || !strings.Contains(isi, awal) {
		t.Fatalf("blok tidak idempoten atau menimpa isi lama: %q", isi)
	}
}
