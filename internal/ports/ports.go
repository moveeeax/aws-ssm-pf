// Package ports helps choose a free local TCP port for the forward.
package ports

import (
	"fmt"
	"net"
)

// IsFree reports whether a TCP port on localhost can currently be bound.
func IsFree(port int) bool {
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return false
	}
	_ = ln.Close()
	return true
}

// Free asks the kernel for an unused ephemeral port.
func Free() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port, nil
}

// Pick returns preferred when it is free (and non-zero); otherwise it returns a
// kernel-assigned free port. The bool reports whether the preferred port was
// substituted.
func Pick(preferred int) (int, bool, error) {
	if preferred > 0 && IsFree(preferred) {
		return preferred, false, nil
	}
	p, err := Free()
	if err != nil {
		return 0, false, err
	}
	return p, preferred > 0, nil
}
