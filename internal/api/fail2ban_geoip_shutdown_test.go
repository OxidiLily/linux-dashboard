package api

import (
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestGeoIPCloseCancelsActiveProvider(t *testing.T) {
	started := make(chan struct{})
	canceled := make(chan struct{})
	w, st := geoFixture(t, func(rw http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
		close(canceled)
	})
	w.items([]string{"8.8.8.8", "1.1.1.1"})
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("provider not started")
	}
	start := time.Now()
	w.close()
	if time.Since(start) > time.Second {
		t.Fatal("close waited for provider timeout instead of canceling")
	}
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("provider not canceled")
	}
	select {
	case <-w.done:
	default:
		t.Fatal("worker still running")
	}
	if _, ok, err := st.GeoIP("8.8.8.8"); err != nil || ok {
		t.Fatalf("canceled lookup cached: %v %v", ok, err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	w.close()
	if got := w.items([]string{"9.9.9.9"}); got.Pending || len(got.Items) != 0 {
		t.Fatalf("closed worker accepted lookup: %+v", got)
	}
}

func TestServerCloseStopsGeoIP(t *testing.T) {
	w, st := geoFixture(t, func(rw http.ResponseWriter, r *http.Request) { t.Error("closed server performed lookup") })
	s := &Server{store: st, geoip: w}
	for i := 0; i < 2; i++ {
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
	}
	select {
	case <-w.done:
	default:
		t.Fatal("server left worker alive")
	}
	if _, _, err := st.GeoIP("8.8.8.8"); err != nil {
		t.Fatal("server closed caller-owned database", err)
	}
}

func TestMainStopsGeoIPAfterHTTPShutdown(t *testing.T) {
	b, err := os.ReadFile("../../cmd/server/main.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(b)
	httpStop := strings.Index(source, "httpSrv.Shutdown(shutdownCtx)")
	workerStop := strings.Index(source, "srv.Close()")
	if httpStop < 0 || workerStop <= httpStop {
		t.Fatal("main must stop API worker after HTTP shutdown, before deferred database close")
	}
}

func TestGeoIPCloseRacesStart(t *testing.T) {
	for i := 0; i < 10; i++ {
		w, _ := geoFixture(t, func(rw http.ResponseWriter, r *http.Request) { rw.Write([]byte(`{"success":false}`)) })
		var wg sync.WaitGroup
		for j := 0; j < 8; j++ {
			wg.Add(1)
			go func() { defer wg.Done(); w.items([]string{"8.8.8.8"}); w.close() }()
		}
		wg.Wait()
		if got := w.items([]string{"1.1.1.1"}); got.Pending || len(got.Items) != 0 {
			t.Fatal("enqueue after close")
		}
	}
}
