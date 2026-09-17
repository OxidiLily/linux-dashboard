package helper

import (
	"bufio"
	"errors"
	"strings"
	"testing"
)

func TestBacaFrameMenolakRequestDiAtasBatas(t *testing.T) {
	_, err := bacaFrame(bufio.NewReader(strings.NewReader(strings.Repeat("x", batasFrameHelper+1) + "\n")))
	if !errors.Is(err, errFrameTerlaluBesar) {
		t.Fatalf("frame besar harus ditolak dengan errFrameTerlaluBesar, dapat %v", err)
	}
}

func TestBacaFrameMenerimaRequestDalamBatas(t *testing.T) {
	ingin := "signature {\"cmd\":\"x\"}\n"
	dapat, err := bacaFrame(bufio.NewReader(strings.NewReader(ingin)))
	if err != nil {
		t.Fatalf("frame sah ditolak: %v", err)
	}
	if string(dapat) != ingin {
		t.Fatalf("frame = %q, harap %q", dapat, ingin)
	}
}
