package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/inful/mdfm"
)

var version = "dev"

const exitUsage = 2

type repeatedFlag []string

func (r *repeatedFlag) String() string {
	return strings.Join(*r, ",")
}

func (r *repeatedFlag) Set(value string) error {
	*r = append(*r, value)
	return nil
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	var setPairs repeatedFlag
	var deleteKeys repeatedFlag
	var showVersion bool

	flagSet := flag.NewFlagSet("mdfm", flag.ContinueOnError)
	flagSet.SetOutput(stderr)
	flagSet.Var(&setPairs, "set", "Set frontmatter key=value (repeatable)")
	flagSet.Var(&deleteKeys, "delete", "Delete frontmatter key (repeatable)")
	flagSet.BoolVar(&showVersion, "version", false, "Show version")
	if err := flagSet.Parse(args); err != nil {
		return 1
	}

	if showVersion {
		_, _ = fmt.Fprintln(stdout, version)
		return 0
	}

	if flagSet.NArg() != 1 {
		_, _ = fmt.Fprintf(stderr, "usage: %s [--set key=value] [--delete key] <file>\n", flagSet.Name())
		return exitUsage
	}

	path := flagSet.Arg(0)

	err := mdfm.UpdateFile(path, func(doc *mdfm.Document) error {
		for _, pair := range setPairs {
			key, value, ok := strings.Cut(pair, "=")
			if !ok {
				return fmt.Errorf("invalid --set value %q, expected key=value", pair)
			}
			if err := doc.Set(strings.TrimSpace(key), strings.TrimSpace(value)); err != nil {
				return err
			}
		}

		for _, key := range deleteKeys {
			if _, err := doc.Delete(strings.TrimSpace(key)); err != nil {
				return err
			}
		}

		return nil
	})
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}

	return 0
}
