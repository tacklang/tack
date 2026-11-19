package porttest

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
)

// lexProbesStage is the stage of the programs and command lines in
// testdata/lex-probes, whose output a known divergence records and
// which no other stage can run: no file of the corpus can hold them,
// since the corpus is this module's frozen examples, and no other stage
// runs --lex on a file of this module, or the driver with a command line
// of its own. Each .tck file there is run with --lex, as lex-single runs
// the corpus's; each .args file is a command line, one argument a line,
// run as it is. Today they are Unicode 18.0.0's: unicode18-string.tck,
// strings of a rune 18.0.0 assigns, which the port's dump quotes with
// std/unicode, at 18.0.0, and Go's with its unicode package, at 17.0.0,
// and three command lines whose usage errors quote such a rune, a flag's
// value, a second file and a backend's name (see the unicode18 entries
// of testdata/divergences.txtar). main.tck is the file the command lines
// name.
const lexProbesStage = "lex-probes"

// init adds lex-probes to stages.
func init() {
	stages = append(stages, stage{
		name:   lexProbesStage,
		args:   lexProbeArgs,
		inputs: lexProbeInputs,
		stdout: compareBytes,
	})
}

// lexProbeCommands holds the command line of each .args probe, by its
// name, which lexProbeInputs fills.
var lexProbeCommands = struct {
	sync.Mutex
	args map[string][]string
}{args: map[string][]string{}}

// lexProbeArgs returns the command line of in, a case of lex-probes:
// its .args file's, or --lex and the file.
func lexProbeArgs(in input) []string {
	lexProbeCommands.Lock()
	defer lexProbeCommands.Unlock()
	if args, ok := lexProbeCommands.args[in.name]; ok {
		return args
	}
	return []string{"--lex", in.file}
}

// lexProbeInputs returns the probes of testdata/lex-probes, each run
// from that directory, in the order of their names, reading each .args
// file's command line. An .args file that is empty, or whose last line
// has no newline, is an error, so a command line cannot be cut short
// unseen.
func lexProbeInputs() ([]input, error) {
	dir := filepath.Join(env.root, "porttest", "testdata", "lex-probes")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var ins []input
	for _, e := range entries {
		switch {
		case e.IsDir():
		case strings.HasSuffix(e.Name(), ".tck"):
			ins = append(ins, input{name: e.Name(), dir: dir, file: e.Name()})
		case strings.HasSuffix(e.Name(), ".args"):
			data, err := os.ReadFile(filepath.Join(dir, e.Name())) //nolint:gosec // G304: a probe of this module
			if err != nil {
				return nil, err
			}
			if len(data) == 0 || data[len(data)-1] != '\n' {
				return nil, fmt.Errorf("lex probe %s: a command line, one argument a line, each ending in a newline", e.Name())
			}
			args := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
			lexProbeCommands.Lock()
			lexProbeCommands.args[e.Name()] = args
			lexProbeCommands.Unlock()
			ins = append(ins, input{name: e.Name(), dir: dir, file: args[len(args)-1]})
		}
	}
	slices.SortFunc(ins, func(a, b input) int { return strings.Compare(a.name, b.name) })
	return ins, nil
}
