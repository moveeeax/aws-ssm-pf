package ports

import (
	"fmt"
	"net"
	"testing"
)

func TestFreeReturnsUsablePort(t *testing.T) {
	p, err := Free()
	if err != nil {
		t.Fatalf("Free: %v", err)
	}
	if p <= 0 || p > 65535 {
		t.Fatalf("port out of range: %d", p)
	}
	if !IsFree(p) {
		t.Errorf("port %d reported as free but cannot bind", p)
	}
}

func TestPickFallsBackWhenBusy(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	busy := ln.Addr().(*net.TCPAddr).Port

	got, substituted, err := Pick(busy)
	if err != nil {
		t.Fatalf("Pick: %v", err)
	}
	if !substituted {
		t.Error("expected substitution flag when preferred port is busy")
	}
	if got == busy {
		t.Errorf("Pick returned the busy port %d", got)
	}
}

func TestPickUsesFreePreferred(t *testing.T) {
	p, _ := Free()
	got, substituted, err := Pick(p)
	if err != nil {
		t.Fatalf("Pick: %v", err)
	}
	if substituted {
		t.Error("did not expect substitution for a free preferred port")
	}
	if got != p {
		t.Errorf("want preferred %d, got %d", p, got)
	}
}

// An out-of-range preference is a typo, not a hint. Silently substituting a
// random port would put the tunnel somewhere the user never asked for.
func TestPickRejectsOutOfRangePreference(t *testing.T) {
	for _, p := range []int{-1, 65536, 99999} {
		if _, _, err := Pick(p); err == nil {
			t.Errorf("expected error for preferred port %d", p)
		}
	}
}

func TestIsFreeFalseWhenBound(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port
	if IsFree(port) {
		t.Errorf("port %d is bound but IsFree returned true", port)
	}
	_ = fmt.Sprint(port)
}
