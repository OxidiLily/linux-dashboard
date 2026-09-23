// Command lindash-server adalah web app dashboard. Ia berjalan sebagai user
// non-root dan mendelegasikan semua operasi privileged ke helper daemon.
package main

import (
	"context"
	"errors"
	"log"
	"net"
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

	// Panel bicara HTTP polos kalau tidak diberi sertifikat. Di alamat yang
	// bisa dijangkau jaringan lain, itu berarti password dan cookie sesi
	// (termasuk sesi sudo) lewat apa adanya. Peringatannya di sini, bukan
	// larangan: panel di belakang reverse proxy HTTPS memang harus tetap bisa
	// jalan tanpa TLS sendiri, dan operator yang tahu apa yang dilakukannya
	// tidak boleh dihalangi.
	if !cfg.SecureCookie && !bindLoopback(cfg.Listen) {
		log.Printf("PERINGATAN: DASHBOARD_LISTEN=%s tanpa TLS dan tanpa Secure cookie — "+
			"password serta cookie sesi lewat jaringan apa adanya. Pakai HTTPS "+
			"(DASHBOARD_TLS_CERT/DASHBOARD_TLS_KEY) atau taruh di belakang reverse proxy, "+
			"lalu set DASHBOARD_SECURE_COOKIE=true.", cfg.Listen)
	}

	st, err := store.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("gagal membuka database: %v", err)
	}
	defer st.Close()

	hc, err := helperclient.New(cfg.SocketPath, cfg.SecretPath, cfg.LegacySecretPath)
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
		// ReadTimeout sengaja TIDAK dipasang: upload file besar (dibatasi
		// ukuran total di handler, tapi tetap bisa berjalan lama) tidak boleh
		// diputus di tengah jalan. ReadHeaderTimeout tetap ada supaya koneksi
		// yang menggantung sebelum mengirim header tidak menumpuk.
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

// bindLoopback melaporkan apakah alamat bind hanya menerima koneksi dari mesin
// ini. Alamat tanpa host (":1122") berarti SEMUA antarmuka, jadi bukan loopback.
//
// "localhost" diterima sebagai loopback: itu nama yang sama-sama sah untuk
// 127.0.0.1 dan dipakai orang apa adanya di DASHBOARD_LISTEN, dan menganggapnya
// bukan loopback memunculkan peringatan "tanpa TLS" yang salah.
func bindLoopback(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		// Tanpa port sama sekali ("127.0.0.1", "localhost").
		host = addr
	}
	if host == "" {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// purgeSessions membuang session kedaluwarsa DAN catatan log yang sudah lewat
// masa simpannya (notifikasi & operasi file 1 bulan, activity log 2 tahun —
// lihat store.retensi). Sapuan pertama jalan langsung saat start: mesin yang
// mati berbulan-bulan tidak perlu menunggu satu jam dulu sebelum log lamanya
// dibersihkan.
func purgeSessions(ctx context.Context, st *store.Store) {
	st.PurgeExpiredSessions()
	st.PurgeLogLama()
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			st.PurgeExpiredSessions()
			st.PurgeLogLama()
		}
	}
}
