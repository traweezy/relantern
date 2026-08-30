package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/traweezy/relantern/internal/soak"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, time.Now))
}

func run(args []string, stdout io.Writer, stderr io.Writer, now func() time.Time) int {
	flags := flag.NewFlagSet("soakctl validate", flag.ContinueOnError)
	flags.SetOutput(stderr)
	file := flags.String("file", "", "path to the staging soak ledger")
	requirePass := flags.Bool("require-pass", false, "exit unsuccessfully unless the ledger passes")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *file == "" || flags.NArg() != 0 {
		fmt.Fprintln(stderr, "usage: soakctl --file <ledger.json> [--require-pass]")
		return 2
	}

	ledgerFile, err := os.Open(*file)
	if err != nil {
		fmt.Fprintf(stderr, "open soak ledger: %v\n", err)
		return 1
	}
	defer ledgerFile.Close()

	ledger, err := soak.Decode(ledgerFile)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	report, err := soak.Evaluate(ledger, now().UTC())
	if err != nil {
		fmt.Fprintf(stderr, "validate soak ledger: %v\n", err)
		return 1
	}
	if err := soak.VerifyLocalArtifacts(ledger, "."); err != nil {
		fmt.Fprintf(stderr, "verify soak evidence: %v\n", err)
		return 1
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		fmt.Fprintf(stderr, "write soak report: %v\n", err)
		return 1
	}
	if *requirePass && report.Outcome != soak.OutcomePass {
		return 1
	}
	return 0
}
