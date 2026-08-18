package main

import (
	"bytes"
	"errors"
	"flag"
	"strconv"
	"strings"
	"testing"

	"hyperloglog/internal/cms"
	"hyperloglog/internal/hll"
)

// stream builds a newline-delimited input from the given elements.
func stream(items ...string) string {
	return strings.Join(items, "\n") + "\n"
}

// repeated builds a stream where each element appears the given number of
// times.
func repeated(pairs ...any) string {
	var sb strings.Builder
	for i := 0; i+1 < len(pairs); i += 2 {
		item := pairs[i].(string)
		n := pairs[i+1].(int)
		for j := 0; j < n; j++ {
			sb.WriteString(item)
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}

func TestCLICardSubcommand(t *testing.T) {
	input := stream("a", "b", "c", "b", "a", "d")

	var out bytes.Buffer
	if err := run([]string{"card"}, strings.NewReader(input), &out); err != nil {
		t.Fatalf("run card: %v", err)
	}
	got := strings.TrimSpace(out.String())
	if got != "4" {
		t.Fatalf("card printed %q, want \"4\"", got)
	}

	out.Reset()
	if err := run([]string{"card", "-v"}, strings.NewReader(input), &out); err != nil {
		t.Fatalf("run card -v: %v", err)
	}
	report := out.String()
	for _, want := range []string{"lines", "distinct", "precision", "registers", "representation", "sparse", "standard error"} {
		if !strings.Contains(report, want) {
			t.Fatalf("verbose report is missing %q:\n%s", want, report)
		}
	}
	if !strings.Contains(report, "lines           6") {
		t.Fatalf("verbose report should count 6 lines:\n%s", report)
	}

	out.Reset()
	if err := run([]string{"card", "-p", "10"}, strings.NewReader(input), &out); err != nil {
		t.Fatalf("run card -p 10: %v", err)
	}
	if got := strings.TrimSpace(out.String()); got != "4" {
		t.Fatalf("card -p 10 printed %q, want \"4\"", got)
	}

	out.Reset()
	if err := run([]string{"card", "-p=12"}, strings.NewReader(input), &out); err != nil {
		t.Fatalf("run card -p=12: %v", err)
	}
	if got := strings.TrimSpace(out.String()); got != "4" {
		t.Fatalf("card -p=12 printed %q, want \"4\"", got)
	}

	out.Reset()
	if err := run([]string{"card"}, strings.NewReader(""), &out); err != nil {
		t.Fatalf("run card on empty input: %v", err)
	}
	if got := strings.TrimSpace(out.String()); got != "0" {
		t.Fatalf("card on empty input printed %q, want \"0\"", got)
	}

	out.Reset()
	if err := run([]string{"card", "-p", "2"}, strings.NewReader(input), &out); !errors.Is(err, hll.ErrPrecisionRange) {
		t.Fatalf("card -p 2 = %v, want ErrPrecisionRange", err)
	}
	if err := run([]string{"card", "extra"}, strings.NewReader(input), &out); err == nil {
		t.Fatal("card must reject a positional argument")
	}
	if err := run([]string{"card", "-nope"}, strings.NewReader(input), &out); err == nil {
		t.Fatal("card must reject an unknown flag")
	}
}

func TestCLICmsSubcommand(t *testing.T) {
	input := repeated("alpha", 12, "beta", 4, "gamma", 1)

	var out bytes.Buffer
	if err := run([]string{"cms", "alpha"}, strings.NewReader(input), &out); err != nil {
		t.Fatalf("run cms: %v", err)
	}
	if got := strings.TrimSpace(out.String()); got != "12" {
		t.Fatalf("cms alpha printed %q, want \"12\"", got)
	}

	out.Reset()
	if err := run([]string{"cms", "absent"}, strings.NewReader(input), &out); err != nil {
		t.Fatalf("run cms: %v", err)
	}
	if got := strings.TrimSpace(out.String()); got != "0" {
		t.Fatalf("cms on an unseen item printed %q, want \"0\"", got)
	}

	// The item may sit before or after the flags.
	for _, args := range [][]string{
		{"cms", "-w", "4096", "beta"},
		{"cms", "beta", "-w", "4096"},
		{"cms", "-w=4096", "beta"},
		{"cms", "beta", "-d", "7", "-w", "8192"},
		{"cms", "-d", "7", "beta", "-w", "8192"},
	} {
		out.Reset()
		if err := run(args, strings.NewReader(input), &out); err != nil {
			t.Fatalf("run %v: %v", args, err)
		}
		if got := strings.TrimSpace(out.String()); got != "4" {
			t.Fatalf("run %v printed %q, want \"4\"", args, got)
		}
	}

	// A leading dash in the item is only safe after the -- terminator.
	out.Reset()
	dashed := repeated("-weird", 3)
	if err := run([]string{"cms", "--", "-weird"}, strings.NewReader(dashed), &out); err != nil {
		t.Fatalf("run cms -- -weird: %v", err)
	}
	if got := strings.TrimSpace(out.String()); got != "3" {
		t.Fatalf("cms -- -weird printed %q, want \"3\"", got)
	}

	if err := run([]string{"cms"}, strings.NewReader(input), &out); err == nil {
		t.Fatal("cms must require an item")
	}
	if err := run([]string{"cms", "a", "b"}, strings.NewReader(input), &out); err == nil {
		t.Fatal("cms must reject two items")
	}
	if err := run([]string{"cms", "-w", "0", "alpha"}, strings.NewReader(input), &out); !errors.Is(err, cms.ErrWidthRange) {
		t.Fatalf("cms -w 0 = %v, want ErrWidthRange", err)
	}
	if err := run([]string{"cms", "-d", "0", "alpha"}, strings.NewReader(input), &out); !errors.Is(err, cms.ErrDepthRange) {
		t.Fatalf("cms -d 0 = %v, want ErrDepthRange", err)
	}
	huge := strconv.FormatUint(uint64(cms.MaxWidth)+1, 10)
	if err := run([]string{"cms", "-w", huge, "alpha"}, strings.NewReader(input), &out); !errors.Is(err, cms.ErrWidthRange) {
		t.Fatalf("cms -w %s = %v, want ErrWidthRange", huge, err)
	}
}

func TestCLITopKSubcommand(t *testing.T) {
	input := repeated("hot", 700, "warm", 200, "mild", 60, "cold", 40)

	var out bytes.Buffer
	if err := run([]string{"topk", "0.15"}, strings.NewReader(input), &out); err != nil {
		t.Fatalf("run topk: %v", err)
	}
	lines := nonEmptyLines(out.String())
	if len(lines) != 2 {
		t.Fatalf("topk 0.15 printed %d rows, want 2:\n%s", len(lines), out.String())
	}
	if !strings.HasPrefix(lines[0], "hot") {
		t.Fatalf("first row = %q, want hot first", lines[0])
	}
	if !strings.HasPrefix(lines[1], "warm") {
		t.Fatalf("second row = %q, want warm second", lines[1])
	}
	if !strings.Contains(lines[0], "700") {
		t.Fatalf("first row = %q, want the estimate 700", lines[0])
	}
	if !strings.Contains(lines[0], "70.000%") {
		t.Fatalf("first row = %q, want a 70%% share", lines[0])
	}

	out.Reset()
	if err := run([]string{"topk", "-n", "1", "0"}, strings.NewReader(input), &out); err != nil {
		t.Fatalf("run topk -n 1: %v", err)
	}
	if got := len(nonEmptyLines(out.String())); got != 1 {
		t.Fatalf("topk -n 1 printed %d rows, want 1", got)
	}

	out.Reset()
	if err := run([]string{"topk", "0", "-n", "3"}, strings.NewReader(input), &out); err != nil {
		t.Fatalf("run topk with a trailing flag: %v", err)
	}
	if got := len(nonEmptyLines(out.String())); got != 3 {
		t.Fatalf("topk 0 -n 3 printed %d rows, want 3", got)
	}

	out.Reset()
	if err := run([]string{"topk", "0.99"}, strings.NewReader(input), &out); err != nil {
		t.Fatalf("run topk 0.99: %v", err)
	}
	if got := len(nonEmptyLines(out.String())); got != 0 {
		t.Fatalf("topk 0.99 printed %d rows, want 0", got)
	}

	out.Reset()
	if err := run([]string{"topk", "0.1"}, strings.NewReader(""), &out); err != nil {
		t.Fatalf("run topk on empty input: %v", err)
	}
	if got := len(nonEmptyLines(out.String())); got != 0 {
		t.Fatalf("topk on empty input printed %d rows, want 0", got)
	}

	if err := run([]string{"topk"}, strings.NewReader(input), &out); err == nil {
		t.Fatal("topk must require a phi argument")
	}
	if err := run([]string{"topk", "abc"}, strings.NewReader(input), &out); err == nil {
		t.Fatal("topk must reject a non-numeric phi")
	}
	if err := run([]string{"topk", "1.5"}, strings.NewReader(input), &out); !errors.Is(err, cms.ErrPhiRange) {
		t.Fatalf("topk 1.5 = %v, want ErrPhiRange", err)
	}
	if err := run([]string{"topk", "-n", "-2", "0.1"}, strings.NewReader(input), &out); err == nil {
		t.Fatal("topk must reject a negative -n")
	}
}

func TestCLIDispatchAndUsage(t *testing.T) {
	var out bytes.Buffer

	if err := run(nil, strings.NewReader(""), &out); err == nil {
		t.Fatal("an empty argument list must fail")
	}
	if err := run([]string{"bogus"}, strings.NewReader(""), &out); err == nil {
		t.Fatal("an unknown subcommand must fail")
	} else if !strings.Contains(err.Error(), "bogus") {
		t.Fatalf("error %q should name the offending subcommand", err)
	}

	for _, arg := range []string{"-h", "--help", "help"} {
		if err := run([]string{arg}, strings.NewReader(""), &out); !errors.Is(err, flag.ErrHelp) {
			t.Fatalf("run %q = %v, want flag.ErrHelp", arg, err)
		}
	}

	for _, want := range []string{"card", "cms", "topk", "Usage:", "standard input"} {
		if !strings.Contains(usageText, want) {
			t.Fatalf("usage text is missing %q", want)
		}
	}
}

func TestCLIReorderArgs(t *testing.T) {
	build := func() *flag.FlagSet {
		fs := newFlagSet("probe")
		fs.Uint("w", 1, "width")
		fs.Bool("v", false, "verbose")
		return fs
	}

	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{"already ordered", []string{"-w", "8", "item"}, []string{"-w", "8", "--", "item"}},
		{"trailing flag", []string{"item", "-w", "8"}, []string{"-w", "8", "--", "item"}},
		{"inline value", []string{"item", "-w=8"}, []string{"-w=8", "--", "item"}},
		{"bool flag keeps its neighbour", []string{"item", "-v", "other"}, []string{"-v", "--", "item", "other"}},
		{"terminator", []string{"--", "-w", "8"}, []string{"--", "-w", "8"}},
		{"only positionals", []string{"a", "b"}, []string{"--", "a", "b"}},
		{"empty", nil, nil},
		{"only flags", []string{"-v"}, []string{"-v"}},
		{"lone dash stays positional", []string{"-", "item"}, []string{"--", "-", "item"}},
		{"dangling flag", []string{"item", "-w"}, []string{"-w", "--", "item"}},
	}

	for _, tc := range cases {
		got := reorder(build(), tc.in)
		if len(got) != len(tc.want) {
			t.Fatalf("%s: reorder(%v) = %v, want %v", tc.name, tc.in, got, tc.want)
		}
		for i := range tc.want {
			if got[i] != tc.want[i] {
				t.Fatalf("%s: reorder(%v) = %v, want %v", tc.name, tc.in, got, tc.want)
			}
		}
	}

	fs := build()
	if err := fs.Parse(reorder(fs, []string{"item", "-w", "16", "-v"})); err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if fs.NArg() != 1 || fs.Arg(0) != "item" {
		t.Fatalf("positional args = %v, want [item]", fs.Args())
	}
	if got := fs.Lookup("w").Value.String(); got != "16" {
		t.Fatalf("-w = %q, want \"16\"", got)
	}
	if got := fs.Lookup("v").Value.String(); got != "true" {
		t.Fatalf("-v = %q, want \"true\"", got)
	}

	if got := flagName("-w=8"); got != "w" {
		t.Fatalf("flagName(-w=8) = %q, want \"w\"", got)
	}
	if got := flagName("--width"); got != "width" {
		t.Fatalf("flagName(--width) = %q, want \"width\"", got)
	}
	if !hasInlineValue("-w=8") {
		t.Fatal("hasInlineValue(-w=8) must be true")
	}
	if hasInlineValue("-w") {
		t.Fatal("hasInlineValue(-w) must be false")
	}
	if !isBoolFlag(build(), "v") {
		t.Fatal("isBoolFlag(v) must be true")
	}
	if isBoolFlag(build(), "w") {
		t.Fatal("isBoolFlag(w) must be false")
	}
	if isBoolFlag(build(), "missing") {
		t.Fatal("isBoolFlag on an unknown flag must be false")
	}
}

// nonEmptyLines splits s into lines, dropping blank ones.
func nonEmptyLines(s string) []string {
	out := make([]string, 0, 8)
	for _, line := range strings.Split(s, "\n") {
		if strings.TrimSpace(line) != "" {
			out = append(out, line)
		}
	}
	return out
}
