package main

import "testing"

const (
	testExpectedErrorGotNil = "expected error, got nil"
	testUnexpectedErrorFmt  = "unexpected error: %v"

	testBucket = "b"
	testPrefix = "p/"
)

func requireErr(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal(testExpectedErrorGotNil)
	}
}

func TestParseArgsRequiresBucket(t *testing.T) {
	t.Parallel()

	_, err := parseArgs([]string{"--prefix", testPrefix})
	requireErr(t, err)
}

func TestParseArgsRequiresPrefix(t *testing.T) {
	t.Parallel()

	_, err := parseArgs([]string{"--bucket", testBucket})
	if err != nil {
		t.Fatalf(testUnexpectedErrorFmt, err)
	}
}

func TestParseArgsOK(t *testing.T) {
	t.Parallel()

	opts, err := parseArgs([]string{"--bucket", testBucket, "--prefix", testPrefix})
	if err != nil {
		t.Fatalf(testUnexpectedErrorFmt, err)
	}
	if opts.bucket != testBucket || opts.prefix != testPrefix {
		t.Fatalf("unexpected opts: %#v", opts)
	}
}

func TestParseArgsAllowsRootPrefix(t *testing.T) {
	t.Parallel()

	opts, err := parseArgs([]string{"--bucket", testBucket, "--prefix", "/"})
	if err != nil {
		t.Fatalf(testUnexpectedErrorFmt, err)
	}
	if opts.prefix != "/" {
		t.Fatalf("unexpected opts: %#v", opts)
	}
}
