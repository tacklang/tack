//go:build tackfactoracle

package typeck

// factOracle says to check the index of the facts in force against a scan
// of them on every query (see factsAbout), and the index of a fact's
// endings against a scan of them (see reachOracle). It is set only in a
// build with the tag tackfactoracle, which only tests use.
const factOracle = true
