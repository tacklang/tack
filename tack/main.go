package main

import (
	"flag"
	"fmt"
	"math"
	"os"
	"os/exec"
	"strings"
	"uuid"

	"github.com/tacklang/tack/codegen"
	// Code generation backends register themselves from init. Importing them
	// here is the whole of what plugging one in requires.
	_ "github.com/tacklang/tack/codegen/c"
	"github.com/tacklang/tack/diag"
	"github.com/tacklang/tack/lexer"
	"github.com/tacklang/tack/parser"
	"github.com/tacklang/tack/resolver"
	"github.com/tacklang/tack/source"
	"github.com/tacklang/tack/typeck"
)

// defaultBackend is the backend used when --backend is not given.
const defaultBackend = "c"

// options holds the command-line flags.
type options struct {
	backendName  string
	outBin       string
	maxDiags     int
	emitLex      bool
	emitAst      bool
	emitGen      bool
	emitRes      bool
	listBackends bool
}

// parseFlags reads the command-line flags into an options.
func parseFlags() options {
	var opts options
	flag.BoolVar(&opts.emitLex, "lex", false, "emit lex")
	flag.BoolVar(&opts.emitAst, "ast", false, "emit AST")
	flag.BoolVar(&opts.emitGen, "emit", false, "emit generated code")
	flag.BoolVar(&opts.emitRes, "res", false, "emit resolved AST")
	flag.StringVar(&opts.backendName, "backend", defaultBackend, "code generation backend to use")
	flag.BoolVar(
		&opts.listBackends,
		"backends",
		false,
		"list the available code generation backends",
	)
	flag.IntVar(&opts.maxDiags, "max-diags", 0, "")
	flag.StringVar(&opts.outBin, "out", "", "path to output binary")
	flag.Parse()
	return opts
}

// emitsOnly reports whether a flag asks for an intermediate form to be
// printed, in which case no binary is required.
func (o options) emitsOnly() bool {
	return o.emitGen || o.emitAst || o.emitLex || o.emitRes
}

func main() {
	opts := parseFlags()

	if opts.listBackends {
		for _, name := range codegen.Names() {
			fmt.Println(name)
		}
		return
	}

	backend, known := codegen.Get(opts.backendName)
	if !known {
		fmt.Printf(
			"unknown backend %q; available: %s\n",
			opts.backendName,
			strings.Join(codegen.Names(), ", "),
		)
		os.Exit(1)
	}

	fname := flag.Arg(0)
	if fname == "" || (opts.outBin == "" && !opts.emitsOnly()) {
		fmt.Println("usage: tack --out <OUT> [OPTIONS...] <FILE>")
		os.Exit(1)
	}

	// Reading the file the user named is the point of a compiler.
	content, err := os.ReadFile(fname) //nolint:gosec // G304: path comes from the user by design
	if err != nil {
		fmt.Printf("failed to open file: %v\n", err)
		os.Exit(1)
	}

	srcFile := &source.File{
		ID:       source.FileID(uuid.New().String()),
		Filename: fname,
		Data:     content,
	}

	out, emitted := compile(srcFile, backend, opts)
	if emitted {
		return
	}

	if !build(backend, out, opts.outBin) {
		os.Exit(1)
	}
}

// compile runs the pipeline from lexing through code generation and returns
// the generated source. Diagnostics from any stage end the process. If a flag
// asked for an intermediate form, compile prints it, stops at that stage, and
// reports emitted as true.
func compile(
	srcFile *source.File,
	backend codegen.Backend,
	opts options,
) (out []byte, emitted bool) {
	lex := lexer.New(srcFile)
	renderDiagnosticsAndExit(srcFile, lex.Lex(), opts.maxDiags)
	tokens := lex.Tokens()

	if opts.emitLex {
		fmt.Println("==================================================")
		for _, token := range tokens {
			minW := math.Floor(math.Log10(float64(tokens[len(tokens)-1].OffsetEnd()))) + 1
			fmt.Println(token.StringPadded(int(minW)))
		}
		fmt.Println("==================================================")
		return nil, true
	}

	p := parser.NewParser(srcFile.ID, tokens)

	file, diags := p.Parse()
	renderDiagnosticsAndExit(srcFile, diags, opts.maxDiags)

	if opts.emitAst {
		printBanner(file.String())
		return nil, true
	}

	res := resolver.New(file)
	renderDiagnosticsAndExit(srcFile, res.Resolve(), opts.maxDiags)

	if opts.emitRes {
		printBanner(file.String())
		return nil, true
	}

	renderDiagnosticsAndExit(srcFile, typeck.New(file).Check(), opts.maxDiags)

	out, diags = backend.Generate(file)
	renderDiagnosticsAndExit(srcFile, diags, opts.maxDiags)

	if opts.emitGen {
		printBanner(string(out))
		return nil, true
	}

	return out, false
}

// printBanner prints s between two separator lines.
func printBanner(s string) {
	fmt.Println("==================================================")
	fmt.Println(s)
	fmt.Println("==================================================")
}

// build writes the generated source to a temporary file and runs the
// backend's build command on it to produce outBin. It reports whether the
// binary was built. The temporary file is removed on every path, which is
// why failure is returned rather than exiting here.
func build(backend codegen.Backend, out []byte, outBin string) bool {
	tmpFile, err := os.CreateTemp("", "tk-*"+backend.Ext())
	if err != nil {
		fmt.Printf("failed to create temp file: %v\n", err)
		return false
	}
	defer func() {
		if err := os.Remove(tmpFile.Name()); err != nil {
			fmt.Printf("failed to delete temp file: %v\n", err)
		}
	}()
	defer func() {
		if err := tmpFile.Close(); err != nil {
			fmt.Printf("failed to close temp file: %v\n", err)
		}
	}()

	if _, err := tmpFile.Write(out); err != nil {
		fmt.Printf("failed to write file: %v\n", err)
		return false
	}

	argv := backend.BuildCmd(tmpFile.Name(), outBin)
	// The command is the backend's own toolchain invocation; only the paths
	// come from the user.
	cmd := exec.Command(argv[0], argv[1:]...) //nolint:gosec // G204: argv is fixed by the backend
	cmd.Stderr = os.Stderr
	// The toolchain has already written its own errors to stderr.
	return cmd.Run() == nil
}

// renderDiagnosticsAndExit prints diags, at most maxDiags of them when
// maxDiags is positive, and exits with status 1. It does nothing when diags
// is empty.
func renderDiagnosticsAndExit(file *source.File, diags []diag.Diagnostic, maxDiags int) {
	if len(diags) > 0 {
		if maxDiags > 0 && len(diags) > maxDiags {
			diags = diags[:maxDiags]
		}
		fmt.Print(diag.RenderAllPretty(file, diags))
		os.Exit(1)
	}
}
