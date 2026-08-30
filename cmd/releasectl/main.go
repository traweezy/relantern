package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/traweezy/relantern/internal/release"
)

func main() {
	os.Exit(run(context.Background(), os.Args[1:], os.Stdout, os.Stderr, time.Now))
}

func run(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer, now func() time.Time) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: releasectl {source|tree|evidence|integrity|tag|provenance|describe} [flags]")
		return 2
	}
	switch args[0] {
	case "source":
		return runSource(args[1:], stdout, stderr)
	case "tree":
		return runTree(ctx, args[1:], stdout, stderr)
	case "evidence":
		return runEvidence(args[1:], stdout, stderr, now)
	case "integrity":
		return runIntegrity(args[1:], stdout, stderr, now)
	case "tag":
		return runTag(args[1:], stdout, stderr)
	case "provenance":
		return runProvenance(args[1:], stdout, stderr, now)
	case "describe":
		return runDescribe(args[1:], stdout, stderr, now)
	default:
		fmt.Fprintf(stderr, "unknown releasectl command %q\n", args[0])
		return 2
	}
}

func runTag(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("releasectl tag", flag.ContinueOnError)
	flags.SetOutput(stderr)
	referencePath := flags.String("reference", "", "GitHub tag reference JSON path")
	tagObjectPath := flags.String("tag-object", "", "GitHub annotated tag JSON path")
	tag := flags.String("tag", "", "expected stable release tag")
	commit := flags.String("commit", "", "expected full release commit SHA")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 || *referencePath == "" || *tagObjectPath == "" {
		fmt.Fprintln(stderr, "usage: releasectl tag --reference <json> --tag-object <json> --tag <tag> --commit <sha>")
		return 2
	}
	referenceFile, err := os.Open(*referencePath)
	if err != nil {
		fmt.Fprintf(stderr, "open tag reference: %v\n", err)
		return 1
	}
	defer referenceFile.Close()
	tagFile, err := os.Open(*tagObjectPath)
	if err != nil {
		fmt.Fprintf(stderr, "open tag object: %v\n", err)
		return 1
	}
	defer tagFile.Close()
	report, err := release.ValidateSignedTag(referenceFile, tagFile, *tag, *commit)
	if err != nil {
		fmt.Fprintf(stderr, "validate signed release tag: %v\n", err)
		return 1
	}
	return writeJSON(stdout, stderr, report)
}

func runProvenance(args []string, stdout io.Writer, stderr io.Writer, now func() time.Time) int {
	flags := flag.NewFlagSet("releasectl provenance", flag.ContinueOnError)
	flags.SetOutput(stderr)
	manifestPath := flags.String("manifest", "", "release manifest path")
	repositoryRoot := flags.String("repository-root", ".", "repository root")
	repository := flags.String("repository", "", "owner/repository identity")
	builderID := flags.String("builder-id", "", "trusted builder identity")
	invocationID := flags.String("invocation-id", "", "unique build invocation identity")
	archivePath := flags.String("archive", "", "immutable source archive path")
	sbomPath := flags.String("sbom", "", "SPDX SBOM path")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 || *manifestPath == "" ||
		*archivePath == "" || *sbomPath == "" {
		fmt.Fprintln(stderr, "usage: releasectl provenance --manifest <path> --archive <path> --sbom <path> --repository <owner/repo> --builder-id <id> --invocation-id <id> [--repository-root <path>]")
		return 2
	}
	manifest, err := release.LoadManifest(*manifestPath)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if _, err := release.ValidateEvidence(manifest, *repositoryRoot, now().UTC()); err != nil {
		fmt.Fprintf(stderr, "validate release evidence before provenance: %v\n", err)
		return 1
	}
	archiveDigest, err := hashFile(*archivePath)
	if err != nil {
		fmt.Fprintf(stderr, "hash source archive: %v\n", err)
		return 1
	}
	sbomDigest, err := hashFile(*sbomPath)
	if err != nil {
		fmt.Fprintf(stderr, "hash release SBOM: %v\n", err)
		return 1
	}
	statement, err := release.NewProvenance(
		manifest, *repository, *builderID, *invocationID,
		filepath.Base(*archivePath), archiveDigest, filepath.Base(*sbomPath), sbomDigest, now().UTC(),
	)
	if err != nil {
		fmt.Fprintf(stderr, "create release provenance: %v\n", err)
		return 1
	}
	return writeJSON(stdout, stderr, statement)
}

func runSource(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("releasectl source", flag.ContinueOnError)
	flags.SetOutput(stderr)
	eventName := flags.String("event-name", "", "GitHub event name")
	baseRef := flags.String("base-ref", "", "pull request base ref")
	headRef := flags.String("head-ref", "", "pull request head ref")
	baseRepository := flags.String("base-repository", "", "base repository name")
	headRepository := flags.String("head-repository", "", "head repository name")
	draft := flags.Bool("draft", false, "whether the pull request is a draft")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 {
		return 2
	}
	input := release.SourceInput{
		EventName: *eventName, BaseRef: *baseRef, HeadRef: *headRef,
		BaseRepository: *baseRepository, HeadRepository: *headRepository, Draft: *draft,
	}
	if err := release.ValidateSource(input); err != nil {
		fmt.Fprintf(stderr, "validate release source: %v\n", err)
		return 1
	}
	return writeJSON(stdout, stderr, map[string]string{"source": "staging", "target": "master"})
}

func runTree(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("releasectl tree", flag.ContinueOnError)
	flags.SetOutput(stderr)
	manifestPath := flags.String("manifest", "", "release manifest path")
	repositoryRoot := flags.String("repository-root", ".", "repository root")
	stagingCommit := flags.String("staging-commit", "", "full staging head SHA")
	candidateCommit := flags.String("candidate-commit", "", "full promotion candidate SHA")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 || *manifestPath == "" ||
		*stagingCommit == "" || *candidateCommit == "" {
		fmt.Fprintln(stderr, "usage: releasectl tree --manifest <path> --staging-commit <sha> --candidate-commit <sha> [--repository-root <path>]")
		return 2
	}
	manifest, err := release.LoadManifest(*manifestPath)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if err := release.ValidateManifestPath(manifest, *manifestPath); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	report, err := release.ValidateTree(ctx, *repositoryRoot, manifest, *stagingCommit, *candidateCommit)
	if err != nil {
		fmt.Fprintf(stderr, "validate release tree: %v\n", err)
		return 1
	}
	return writeJSON(stdout, stderr, report)
}

func runEvidence(args []string, stdout io.Writer, stderr io.Writer, now func() time.Time) int {
	flags := flag.NewFlagSet("releasectl evidence", flag.ContinueOnError)
	flags.SetOutput(stderr)
	manifestPath := flags.String("manifest", "", "release manifest path")
	repositoryRoot := flags.String("repository-root", ".", "repository root")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 || *manifestPath == "" {
		fmt.Fprintln(stderr, "usage: releasectl evidence --manifest <path> [--repository-root <path>]")
		return 2
	}
	manifest, err := release.LoadManifest(*manifestPath)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if err := release.ValidateManifestPath(manifest, *manifestPath); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	report, err := release.ValidateEvidence(manifest, *repositoryRoot, now().UTC())
	if err != nil {
		fmt.Fprintf(stderr, "validate release evidence: %v\n", err)
		return 1
	}
	return writeJSON(stdout, stderr, report)
}

func runIntegrity(args []string, stdout io.Writer, stderr io.Writer, now func() time.Time) int {
	flags := flag.NewFlagSet("releasectl integrity", flag.ContinueOnError)
	flags.SetOutput(stderr)
	pullsPath := flags.String("pulls", "", "associated pull request response path")
	repositoryRoot := flags.String("repository-root", ".", "repository root")
	repository := flags.String("repository", "", "owner/repository identity")
	commitSHA := flags.String("commit", "", "full master commit SHA")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 || *pullsPath == "" {
		fmt.Fprintln(stderr, "usage: releasectl integrity --pulls <json> --repository <owner/repo> --commit <sha> [--repository-root <path>]")
		return 2
	}
	file, err := release.OpenPullRequests(*pullsPath)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer file.Close()
	report, err := release.ValidateIntegrity(file, *repositoryRoot, *repository, *commitSHA, now().UTC())
	if err != nil {
		fmt.Fprintf(stderr, "validate master integrity: %v\n", err)
		return 1
	}
	return writeJSON(stdout, stderr, report)
}

func runDescribe(args []string, stdout io.Writer, stderr io.Writer, now func() time.Time) int {
	flags := flag.NewFlagSet("releasectl describe", flag.ContinueOnError)
	flags.SetOutput(stderr)
	manifestPath := flags.String("manifest", "", "release manifest path")
	field := flags.String("field", "", "tag, approved-release-sha, or approved-tree-sha")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 || *manifestPath == "" {
		return 2
	}
	manifest, err := release.LoadManifest(*manifestPath)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if err := release.ValidateManifest(manifest, now().UTC()); err != nil {
		fmt.Fprintf(stderr, "validate release manifest before describing it: %v\n", err)
		return 1
	}
	var value string
	switch *field {
	case "tag":
		value = manifest.Tag
	case "approved-release-sha":
		value = manifest.ApprovedReleaseSHA
	case "approved-tree-sha":
		value = manifest.ApprovedTreeSHA
	default:
		fmt.Fprintln(stderr, "describe field must be tag, approved-release-sha, or approved-tree-sha")
		return 2
	}
	fmt.Fprintln(stdout, value)
	return 0
}

func writeJSON(stdout io.Writer, stderr io.Writer, value any) int {
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		fmt.Fprintf(stderr, "write releasectl result: %v\n", err)
		return 1
	}
	return 0
}

func hashFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", hasher.Sum(nil)), nil
}
