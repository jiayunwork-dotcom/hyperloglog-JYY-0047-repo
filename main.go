// Command hyperloglog estimates stream statistics from a list of elements read
// on standard input, one element per line.
//
// Subcommands:
//
//	card              estimate the number of distinct elements
//	cms <item>        estimate how often <item> occurred
//	topk <phi>        list elements whose share of the stream exceeds phi
//
// All of the work is done by the internal packages; this file only parses
// flags, wires the pieces together and formats the result.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"

	"hyperloglog/internal/cms"
	"hyperloglog/internal/hll"
)

const usageText = `hyperloglog - cardinality and frequency sketches over a line-oriented stream

Usage:
  hyperloglog card [-p precision] [-v]
  hyperloglog cms  [-d depth] [-w width] <item>
  hyperloglog topk [-d depth] [-w width] [-n limit] <phi>

Elements are read from standard input, one per line. Surrounding whitespace is
trimmed and blank lines are skipped.

Subcommands:
  card    Estimate the number of distinct elements with a HyperLogLog sketch.
          -p sets the precision; the sketch keeps 2^p registers.
          -v also reports register occupancy and the expected error.

  cms     Estimate how often <item> occurred with a Count-Min sketch.
          The estimate never falls below the true count.

  topk    List the elements whose estimated share of the stream exceeds phi.
          phi is a fraction in [0,1]; -n caps how many rows are printed.
`

func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprint(os.Stdout, usageText)
			return
		}
		fmt.Fprintln(os.Stderr, "hyperloglog:", err)
		os.Exit(1)
	}
}

// run dispatches to a subcommand. It is separate from main so that every exit
// path is an error value rather than a call to os.Exit.
func run(args []string, stdin io.Reader, stdout io.Writer) error {
	if len(args) == 0 {
		return errors.New("missing subcommand (try -h)")
	}

	switch args[0] {
	case "card":
		return runCard(args[1:], stdin, stdout)
	case "cms":
		return runCMS(args[1:], stdin, stdout)
	case "topk":
		return runTopK(args[1:], stdin, stdout)
	case "-h", "--help", "help":
		return flag.ErrHelp
	default:
		return fmt.Errorf("unknown subcommand %q (try -h)", args[0])
	}
}

// reorder moves flags ahead of positional arguments.
//
// The flag package stops at the first argument that does not look like a flag,
// so "cms alpha -w 4096" would otherwise leave -w unparsed. Any argument that
// starts with a single dash is treated as a flag, and it consumes the next
// argument as its value unless it was written in the -name=value form or names
// a boolean flag. An explicit "--" ends flag scanning, so everything after it
// stays positional even if it starts with a dash.
//
// The positional block is always introduced by a "--" terminator. That keeps
// the reordering lossless: a positional argument can never be re-read as a
// flag on the second pass, whatever it looks like.
func reorder(fs *flag.FlagSet, args []string) []string {
	flags := make([]string, 0, len(args))
	positional := make([]string, 0, len(args)+1)

	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			positional = append(positional, args[i+1:]...)
			break
		}
		if len(arg) < 2 || arg[0] != '-' {
			positional = append(positional, arg)
			continue
		}

		flags = append(flags, arg)
		name := flagName(arg)
		if name == "" || hasInlineValue(arg) || isBoolFlag(fs, name) {
			continue
		}
		if i+1 < len(args) {
			i++
			flags = append(flags, args[i])
		}
	}

	if len(positional) == 0 {
		return flags
	}
	return append(append(flags, "--"), positional...)
}

// flagName strips the leading dashes and any inline value from a flag
// argument.
func flagName(arg string) string {
	name := arg
	for len(name) > 0 && name[0] == '-' {
		name = name[1:]
	}
	for i := 0; i < len(name); i++ {
		if name[i] == '=' {
			return name[:i]
		}
	}
	return name
}

// hasInlineValue reports whether the argument already carries its value.
func hasInlineValue(arg string) bool {
	for i := 0; i < len(arg); i++ {
		if arg[i] == '=' {
			return true
		}
	}
	return false
}

// isBoolFlag reports whether the named flag takes no value.
func isBoolFlag(fs *flag.FlagSet, name string) bool {
	f := fs.Lookup(name)
	if f == nil {
		return false
	}
	bf, ok := f.Value.(interface{ IsBoolFlag() bool })
	return ok && bf.IsBoolFlag()
}

// newFlagSet builds a flag set that reports errors instead of exiting.
func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	return fs
}

func runCard(args []string, stdin io.Reader, stdout io.Writer) error {
	fs := newFlagSet("card")
	precision := fs.Uint("p", hll.DefaultPrecision, "register precision")
	verbose := fs.Bool("v", false, "report sketch diagnostics")
	if err := fs.Parse(reorder(fs, args)); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("card takes no positional arguments, got %q", fs.Arg(0))
	}

	sketch, err := hll.New(*precision)
	if err != nil {
		return err
	}
	read, err := sketch.AddLines(stdin)
	if err != nil {
		return err
	}

	if !*verbose {
		fmt.Fprintln(stdout, sketch.Count())
		return nil
	}

	stats := sketch.Stats()
	fmt.Fprintf(stdout, "lines           %d\n", read)
	fmt.Fprintf(stdout, "distinct        %d\n", stats.Estimate)
	fmt.Fprintf(stdout, "precision       %d\n", stats.Precision)
	fmt.Fprintf(stdout, "registers       %d\n", stats.Registers)
	fmt.Fprintf(stdout, "representation  %s\n", representation(stats.Sparse))
	fmt.Fprintf(stdout, "occupied        %d\n", stats.Occupied)
	fmt.Fprintf(stdout, "max register    %d\n", stats.MaxRegister)
	fmt.Fprintf(stdout, "standard error  %.4f%%\n", stats.StandardError*100)
	return nil
}

func representation(sparse bool) string {
	if sparse {
		return "sparse"
	}
	return "dense"
}

func runCMS(args []string, stdin io.Reader, stdout io.Writer) error {
	fs := newFlagSet("cms")
	depth := fs.Uint("d", uint(cms.DefaultDepth), "number of counter rows")
	width := fs.Uint("w", uint(cms.DefaultWidth), "counters per row")
	if err := fs.Parse(reorder(fs, args)); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("cms needs exactly one <item> argument")
	}
	item := fs.Arg(0)
	if item == "" {
		return errors.New("cms needs a non-empty <item>")
	}

	sketch, err := newSketch(*depth, *width)
	if err != nil {
		return err
	}
	if _, err := sketch.AddLines(stdin); err != nil {
		return err
	}

	fmt.Fprintln(stdout, sketch.EstimateString(item))
	return nil
}

func runTopK(args []string, stdin io.Reader, stdout io.Writer) error {
	fs := newFlagSet("topk")
	depth := fs.Uint("d", uint(cms.DefaultDepth), "number of counter rows")
	width := fs.Uint("w", uint(cms.DefaultWidth), "counters per row")
	limit := fs.Int("n", 0, "print at most this many rows (0 means all)")
	if err := fs.Parse(reorder(fs, args)); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("topk needs exactly one <phi> argument")
	}

	phi, err := strconv.ParseFloat(fs.Arg(0), 64)
	if err != nil {
		return fmt.Errorf("invalid phi %q: %w", fs.Arg(0), err)
	}
	if *limit < 0 {
		return fmt.Errorf("invalid -n %d: must not be negative", *limit)
	}

	sketch, err := newSketch(*depth, *width)
	if err != nil {
		return err
	}
	if _, err := sketch.AddLines(stdin); err != nil {
		return err
	}

	hitters, err := sketch.HeavyHitters(phi)
	if err != nil {
		return err
	}
	if *limit > 0 && len(hitters) > *limit {
		hitters = hitters[:*limit]
	}

	for _, h := range hitters {
		fmt.Fprintf(stdout, "%-32s %10d %7.3f%%\n", h.Item, h.Estimate, h.Share*100)
	}
	return nil
}

// newSketch validates the requested matrix shape before handing it to the cms
// package, so that a value too large for uint32 is reported as a range error
// rather than silently wrapping.
func newSketch(depth, width uint) (*cms.Sketch, error) {
	if depth > uint(cms.MaxDepth) {
		return nil, fmt.Errorf("invalid -d %d: %w", depth, cms.ErrDepthRange)
	}
	if width > uint(cms.MaxWidth) {
		return nil, fmt.Errorf("invalid -w %d: %w", width, cms.ErrWidthRange)
	}
	return cms.New(uint32(depth), uint32(width))
}
