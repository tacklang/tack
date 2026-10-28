//go:build linux

package main

import "syscall"

// specParentDeath asks Linux to send a spec binary SIGKILL when the thread
// that started it ends, which runSpecBinary keeps alive for the whole run
// by locking its goroutine to it: so the binary dies with the test
// process however that ends, SIGKILL and a test timeout's panic included.
// Only the binary itself is signaled, not the processes it started.
func specParentDeath(attr *syscall.SysProcAttr) {
	if !specNoParentDeath {
		attr.Pdeathsig = syscall.SIGKILL
	}
}

// specNoParentDeath turns specParentDeath off, for TestSpecTestDies to
// show the SIGINT and SIGTERM handling alone kills the binary, as it
// must where there is no parent-death signal.
var specNoParentDeath bool
