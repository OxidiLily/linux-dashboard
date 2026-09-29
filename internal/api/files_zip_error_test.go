package api

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"linux-dashboard/OxidiLily/internal/helperproto"
)

type zipFailedReader struct{}

func (zipFailedReader) Read(p []byte) (int, error) { return copy(p, "partial"), io.ErrUnexpectedEOF }
func (zipFailedReader) Close() error               { return nil }

func TestZipStopsAfterSourceFailure(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	reads := 0
	src := zipSumber{
		list: func(string) ([]helperproto.FileEntry, error) {
			return []helperproto.FileEntry{{Name: "bad", Path: "/bad"}, {Name: "next", Path: "/next"}}, nil
		},
		read: func(string) (io.ReadCloser, error) { reads++; return zipFailedReader{}, nil },
	}
	if err := zipPath(zw, src, "/root", "root"); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("copy error lost: %v", err)
	}
	if reads != 1 {
		t.Fatalf("source failure ignored: opened %d files", reads)
	}
}

func TestArchiveSourceFailureDoesNotFinalize(t *testing.T) {
	tiruan := &helperTiruan{balasE: errors.New("source failed")}
	router, st := buatServerCron(t, tiruan)
	session := buatSesi(t, st, "ani", true)
	req := httptest.NewRequest(http.MethodGet, "/api/files/archive?path=/bad", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: session})
	w := httptest.NewRecorder()
	aborted := false
	func() {
		defer func() {
			if v := recover(); v != nil {
				if v != http.ErrAbortHandler {
					panic(v)
				}
				aborted = true
			}
		}()
		router.ServeHTTP(w, req)
	}()
	if _, err := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len())); err == nil {
		t.Fatal("failed source produced valid ZIP")
	}
	if strings.Contains(w.Body.String(), "404 page not found") {
		t.Fatal("wrong route")
	}
	if !aborted && w.Code < 400 {
		t.Fatalf("source failure returned HTTP %d", w.Code)
	}
	ops, err := st.FileOps("ani", 10, 0)
	if err != nil || len(ops) != 0 {
		t.Fatalf("failed download logged as success: %v, %v", ops, err)
	}
	activity, err := st.Activity([]string{"file_download"}, "ani", 10, 0)
	if err != nil || len(activity) != 0 {
		t.Fatalf("failed download activity: %v, %v", activity, err)
	}
}

func TestZipSuccessRemainsReadable(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	src := zipSumber{
		list: func(string) ([]helperproto.FileEntry, error) { return nil, errors.New("not directory") },
		read: func(string) (io.ReadCloser, error) { return io.NopCloser(strings.NewReader("complete")), nil },
	}
	if err := zipPath(zw, src, "/file", "file"); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	if len(zr.File) != 1 {
		t.Fatalf("entries: %d", len(zr.File))
	}
	r, err := zr.File[0].Open()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	got, err := io.ReadAll(r)
	if err != nil || string(got) != "complete" {
		t.Fatalf("content: %q, %v", got, err)
	}
}
