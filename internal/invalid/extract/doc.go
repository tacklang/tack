// Command extract prints the program of one case in examples/invalid.txtar,
// byte for byte, so the case can be run by hand:
//
//	go run ./internal/invalid/extract garbage > /tmp/main.tck
//	go run ./tack --emit /tmp/main.tck
//
// It takes the case name and, optionally, the archive's path, which defaults
// to examples/invalid.txtar relative to the working directory (the module
// root under go run). It exits 1 if the archive is malformed or has no such
// case, and 2 on a usage error.
package main
