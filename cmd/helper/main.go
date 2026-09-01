// Command lindash-helper adalah daemon privileged (root) yang mengeksekusi
// operasi sistem atas nama web app. Web app sendiri tidak pernah root.
package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"

	"linux-dashboard/OxidiLily/internal/config"
	"linux-dashboard/OxidiLily/internal/helper"
)

func main() {
	// Mode worker: proses anak yang privilege-nya sudah diturunkan kernel ke
	// user target sebelum exec.
	if len(os.Args) > 1 && os.Args[1] == helper.WorkerArg {
		os.Exit(helper.RunWorker())
	}

	// Mode copot-components: dipanggil uninstall.sh mode "total" supaya
	// pencopotan memakai uninstaller yang sama dengan halaman Components,
	// bukan daftar paket yang ditulis ulang di dalam skrip bash.
	if len(os.Args) > 1 && os.Args[1] == helper.CopotComponentsArg {
		if os.Geteuid() != 0 {
			log.Fatal("copot-components harus dijalankan sebagai root")
		}
		os.Exit(helper.CopotSemuaKomponen())
	}

	log.SetFlags(log.LstdFlags | log.Lmsgprefix)
	log.SetPrefix("[helper] ")

	if os.Geteuid() != 0 {
		log.Fatal("helper daemon harus dijalankan sebagai root")
	}

	cfg := config.Load()
	srv, err := helper.NewServer(cfg.SocketPath, cfg.SecretPath, cfg.SocketGroup)
	if err != nil {
		log.Fatalf("gagal start: %v", err)
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-stop
		log.Println("shutdown")
		_ = srv.Close()
	}()

	if err := srv.Serve(); err != nil {
		log.Printf("serve berhenti: %v", err)
	}
}
