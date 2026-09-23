package main

import "testing"

func TestBindLoopback(t *testing.T) {
	cases := []struct {
		addr string
		mau  bool
	}{
		{"127.0.0.1:8080", true},
		{"127.0.0.1", true},
		{"127.5.5.5:1122", true},
		{"localhost:1122", true},
		{"localhost", true},
		{"[::1]:1122", true},
		{"::1", true},
		{":1122", false},
		{"", false},
		{"0.0.0.0:1122", false},
		{"0.0.0.0", false},
		{"192.168.1.10:1122", false},
		{"localhost.example.com:1122", false},
		{"[::]:1122", false},
	}
	for _, c := range cases {
		if got := bindLoopback(c.addr); got != c.mau {
			t.Errorf("bindLoopback(%q) = %v, mau %v", c.addr, got, c.mau)
		}
	}
}
