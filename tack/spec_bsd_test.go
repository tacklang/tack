//go:build unix && !linux

package main

import "syscall"

// specParentDeath does nothing where, as on macOS, the system cannot
// signal a process when its parent dies: a spec binary outlives a test
// process killed by SIGKILL or ended by a test timeout's panic.
func specParentDeath(*syscall.SysProcAttr) {}
