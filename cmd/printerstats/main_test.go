package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunRejectsInvalidDirectIPWithoutReadingConfig(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"-ip", "not-an-ip"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "not a valid IP address") {
		t.Fatalf("unexpected error: %s", stderr.String())
	}
}

func TestRunRejectsInvalidSNMPVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"-ip", "192.0.2.10", "-version", "3"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "-version must be") {
		t.Fatalf("unexpected error: %s", stderr.String())
	}
}
