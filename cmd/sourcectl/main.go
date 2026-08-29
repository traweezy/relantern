package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/traweezy/relantern/internal/sources"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "sourcectl: %v\n", err)
		os.Exit(1)
	}
}

func run(arguments []string) error {
	if len(arguments) == 0 || arguments[0] != "verify" {
		return fmt.Errorf("usage: sourcectl verify [-registry path] [-fixtures path]")
	}
	flags := flag.NewFlagSet("sourcectl verify", flag.ContinueOnError)
	registryPath := flags.String("registry", "sources/registry.yaml", "path to the reviewed registry")
	fixturesPath := flags.String("fixtures", "sources/fixtures.yaml", "path to deterministic connector fixtures")
	if err := flags.Parse(arguments[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("usage: sourcectl verify [-registry path] [-fixtures path]")
	}

	registry, err := sources.LoadRegistry(*registryPath)
	if err != nil {
		return err
	}
	catalog, err := sources.LoadFixtureCatalog(*fixturesPath)
	if err != nil {
		return err
	}
	if err := sources.Validate(registry, catalog, time.Now()); err != nil {
		return err
	}

	_, _ = fmt.Fprintf(
		os.Stdout,
		"verified %d source endpoints and %d connector fixture suites; live fetching disabled\n",
		len(registry.Endpoints()),
		len(catalog.Suites),
	)
	return nil
}
