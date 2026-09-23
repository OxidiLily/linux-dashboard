package helper

import (
	"strings"
	"testing"

	"linux-dashboard/OxidiLily/internal/helperproto"
)

// Penyaringan antrean cetak diuji lewat fungsi murninya: di mesin uji `lpstat`
// tidak ada, jadi printJobs() selalu kosong dan cabang ini tidak akan pernah
// tercapai lewat jalur IO.
func TestJobMilikUserMenyaringAntrean(t *testing.T) {
	jobs := []helperproto.PrintJob{
		{ID: "hp-1", User: "budi", Size: 10},
		{ID: "hp-2", User: "siti", Size: 20},
		{ID: "hp-3", User: "budi", Size: 30},
	}
	milik := jobMilikUser(jobs, &userInfo{Name: "budi"})
	if len(milik) != 2 {
		t.Fatalf("job milik budi = %d, mau 2", len(milik))
	}
	for _, j := range milik {
		if j.User != "budi" {
			t.Errorf("job %s milik %s ikut terbawa", j.ID, j.User)
		}
	}
	if got := jobMilikUser(jobs, &userInfo{Name: "tidak-ada"}); len(got) != 0 {
		t.Errorf("user tanpa job = %d, mau 0", len(got))
	}
	if got := jobMilikUser(nil, &userInfo{Name: "budi"}); len(got) != 0 {
		t.Errorf("antrean kosong = %d, mau 0", len(got))
	}
}

func TestBolehBatalkanJobMenolakJobMilikUserLain(t *testing.T) {
	jobs := []helperproto.PrintJob{
		{ID: "hp-1", User: "budi"},
		{ID: "hp-2", User: "siti"},
	}
	budi := &userInfo{Name: "budi"}

	if err := bolehBatalkanJob(jobs, budi, "hp-1"); err != nil {
		t.Fatalf("job sendiri ditolak: %v", err)
	}

	err := bolehBatalkanJob(jobs, budi, "hp-2")
	if err == nil {
		t.Fatal("job milik user lain diterima")
	}
	if !strings.Contains(err.Error(), "milik user lain") {
		t.Errorf("pesan penolakan = %q", err)
	}

	err = bolehBatalkanJob(jobs, budi, "hp-9")
	if err == nil {
		t.Fatal("job yang tidak ada di antrean diterima")
	}
	if !strings.Contains(err.Error(), "tidak ada di antrean") {
		t.Errorf("pesan job tak dikenal = %q", err)
	}

	// Tanpa identitas peminta, tidak ada job yang boleh dibatalkan.
	if err := bolehBatalkanJob(jobs, nil, "hp-1"); err == nil {
		t.Error("peminta tanpa identitas boleh membatalkan job")
	}
	if err := bolehBatalkanJob(nil, budi, "hp-1"); err == nil {
		t.Error("antrean kosong malah mengizinkan pembatalan")
	}
}
