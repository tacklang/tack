//go:build !unix

package main

import (
	"fmt"
	"os/exec"
	"testing"
)

// specOwnGroup does nothing where there are no Unix process groups.
func specOwnGroup(*exec.Cmd) {}

// killSpecGroup kills cmd's process alone where there are no Unix process
// groups to kill the processes it started with it.
func killSpecGroup(cmd *exec.Cmd) {
	_ = cmd.Process.Kill() // it may have exited already, which is no matter
}

// startSpecTracked starts cmd, a spec binary, where there are no Unix
// signals to watch and so no groups to record.
func startSpecTracked(cmd *exec.Cmd) error {
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting %s: %w", cmd.Path, err)
	}
	return nil
}

// untrackSpecGroup does nothing where startSpecTracked records nothing.
func untrackSpecGroup(*exec.Cmd) {}

// assertSpecProcessGone does nothing where there is no /proc to look a
// process up in.
func assertSpecProcessGone(*testing.T, string) {}

// assertSpecProcessDead does nothing where there is no /proc to look a
// process up in.
func assertSpecProcessDead(*testing.T, string) {}
