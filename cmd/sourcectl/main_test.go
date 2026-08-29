package main

import (
	"strings"
	"testing"
)

func TestRunVerifiesReviewedFiles(t *testing.T) {
	err := run([]string{
		"verify",
		"-registry", "../../sources/registry.yaml",
		"-fixtures", "../../sources/fixtures.yaml",
	})
	if err != nil {
		t.Fatalf("run() error = %v", err)
	}
}

func TestRunRejectsInvalidInvocation(t *testing.T) {
	for _, arguments := range [][]string{
		nil,
		{"summarize"},
		{"verify", "unexpected"},
	} {
		if err := run(arguments); err == nil || !strings.Contains(err.Error(), "usage: sourcectl verify") {
			t.Fatalf("run(%v) error = %v", arguments, err)
		}
	}
}
