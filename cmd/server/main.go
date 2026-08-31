// Command lindash-server adalah web app dashboard. Ia berjalan sebagai user
// non-root dan mendelegasikan semua operasi privileged ke helper daemon.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"linux-dashboard/OxidiLily/internal/api"
	"linux-dashboard/OxidiLily/internal/config"
	"linux-dashboard/OxidiLily/internal/helperclient"
	"linux-dashboard/OxidiLily/internal/metrics"
	"linux-dashboard/OxidiLily/internal/platform"
	"linux-dashboard/OxidiLily/internal/store"
	"linux-dashboard/OxidiLily/web"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lmsgprefix)
	log.SetPrefix("[lindash] ")

	if os.Geteuid() == 0 {
		log.Println("PERINGATAN: web app sebaiknya TIDAK dijalankan sebagai root — " +
			"operasi privileged sudah ditangani helper daemon")
	}

	cfg := config.Load()

	st, err := store.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("gagal membuka database: %v", err)
	}
	defer st.Close()

	hc, err := helperclient.New(cfg.SocketPath, cfg.SecretPath)
	if err != nil {
		log.Fatalf("gagal menyiapkan client helper: %v", err)
	}

	collector := metrics.NewCollector()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go collector.Run(ctx)
	go purgeSessions(ctx, st)

	srv := api.New(cfg, st, hc, collector, web.Handler())

	httpSrv := &http.Server{
		Addr:    cfg.Listen,
		Handler: srv.Routes(),
		// ReadTimeout sengaja TIDAK dipasang: upload file besar (unlimited)
		// bisa berjalan berjam-jam. ReadHeaderTimeout tetap ada supaya
		// koneksi yang menggantung sebelum mengirim header tidak menumpuk.
		ReadHeaderTimeout: 20 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	p := platform.Detect()
	log.Printf("platform: %s (%s/%s)", p.Display, p.PlatformType, p.Arch)
	log.Printf("mendengarkan di %s", cfg.Listen)

	go func() {
		var err error
		if cfg.TLSCert != "" && cfg.TLSKey != "" {
			err = httpSrv.ListenAndServeTLS(cfg.TLSCert, cfg.TLSKey)
		} else {
			err = httpSrv.ListenAndServe()
		}
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server berhenti: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	log.Println("shutdown…")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	_ = httpSrv.Shutdown(shutdownCtx)
}

func purgeSessions(ctx context.Context, st *store.Store) {
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			st.PurgeExpiredSessions()
		}
	}
}
