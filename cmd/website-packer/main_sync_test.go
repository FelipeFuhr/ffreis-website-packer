package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudfront"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/felipefuhr/ffreis-website-packer/internal/packer"
)

// fakeS3 is a self-contained fake satisfying s3SyncClient — deliberately not
// shared with internal/packer's test fakes (different package, unexported
// types there anyway) but following the same interface-mock convention
// documented in this repo's AGENTS.md.
type fakeS3 struct {
	listErr      error
	putErr       error
	deleteErr    error
	deleteErrors []types.Error
	remoteKeys   []string

	putCalls    []s3.PutObjectInput
	deleteCalls int
}

func (f *fakeS3) ListObjectsV2(_ context.Context, _ *s3.ListObjectsV2Input, _ ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	var contents []types.Object
	for _, k := range f.remoteKeys {
		key := k
		contents = append(contents, types.Object{Key: &key})
	}
	return &s3.ListObjectsV2Output{Contents: contents, IsTruncated: aws.Bool(false)}, nil
}

func (f *fakeS3) PutObject(_ context.Context, params *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	if f.putErr != nil {
		return nil, f.putErr
	}
	if params != nil {
		f.putCalls = append(f.putCalls, *params)
	}
	return &s3.PutObjectOutput{}, nil
}

func (f *fakeS3) DeleteObjects(_ context.Context, _ *s3.DeleteObjectsInput, _ ...func(*s3.Options)) (*s3.DeleteObjectsOutput, error) {
	f.deleteCalls++
	if f.deleteErr != nil {
		return nil, f.deleteErr
	}
	return &s3.DeleteObjectsOutput{Errors: f.deleteErrors}, nil
}

type fakeCF struct {
	err      error
	captured *cloudfront.CreateInvalidationInput
}

func (f *fakeCF) CreateInvalidation(_ context.Context, params *cloudfront.CreateInvalidationInput, _ ...func(*cloudfront.Options)) (*cloudfront.CreateInvalidationOutput, error) {
	f.captured = params
	if f.err != nil {
		return nil, f.err
	}
	return &cloudfront.CreateInvalidationOutput{}, nil
}

func writeSite(t *testing.T, dir string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html></html>"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSyncDryRunPrintsPlanAndSkipsWrites(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeSite(t, dir)

	s3c := &fakeS3{}
	var stdout, stderr bytes.Buffer
	opts := options{bucket: testBucket, prefix: testPrefix, dir: dir, dryRun: true}

	code := sync(context.Background(), opts, s3c, nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%s", code, stderr.String())
	}
	if len(s3c.putCalls) != 0 || s3c.deleteCalls != 0 {
		t.Fatal("dry-run must not perform uploads or deletes")
	}
	if !strings.Contains(stdout.String(), "dry-run") {
		t.Errorf("stdout %q missing dry-run marker", stdout.String())
	}
	if !strings.Contains(stdout.String(), "uploads: 1") {
		t.Errorf("stdout %q missing upload count", stdout.String())
	}
}

func TestSyncAppliesUploadsAndDeletes(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeSite(t, dir)

	s3c := &fakeS3{remoteKeys: []string{testPrefix + "old.html"}}
	var stdout, stderr bytes.Buffer
	opts := options{bucket: testBucket, prefix: testPrefix, dir: dir}

	code := sync(context.Background(), opts, s3c, nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%s", code, stderr.String())
	}
	if len(s3c.putCalls) != 1 {
		t.Fatalf("PutObject calls = %d, want 1", len(s3c.putCalls))
	}
	if s3c.deleteCalls != 1 {
		t.Fatalf("DeleteObjects calls = %d, want 1", s3c.deleteCalls)
	}
	if !strings.Contains(stdout.String(), "done: uploaded=1 deleted=1") {
		t.Errorf("stdout %q missing done summary", stdout.String())
	}
}

func TestSyncNoDeleteSkipsRemoteExtras(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeSite(t, dir)

	s3c := &fakeS3{remoteKeys: []string{testPrefix + "old.html"}}
	var stdout, stderr bytes.Buffer
	opts := options{bucket: testBucket, prefix: testPrefix, dir: dir, noDelete: true}

	code := sync(context.Background(), opts, s3c, nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%s", code, stderr.String())
	}
	if s3c.deleteCalls != 0 {
		t.Fatalf("DeleteObjects calls = %d, want 0 with --no-delete", s3c.deleteCalls)
	}
}

func TestSyncInvalidPrefixReturns2(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	opts := options{bucket: testBucket, prefix: "   ", dir: t.TempDir()}

	code := sync(context.Background(), opts, &fakeS3{}, nil, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
}

func TestSyncDiscoveryFailureReturns1(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	opts := options{bucket: testBucket, prefix: testPrefix, dir: filepath.Join(t.TempDir(), "missing")}

	code := sync(context.Background(), opts, &fakeS3{}, nil, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "website discovery failed") {
		t.Errorf("stderr %q missing discovery-failure message", stderr.String())
	}
}

func TestSyncListRemoteFailureReturns1(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeSite(t, dir)

	s3c := &fakeS3{listErr: errors.New("boom")}
	var stdout, stderr bytes.Buffer
	opts := options{bucket: testBucket, prefix: testPrefix, dir: dir}

	code := sync(context.Background(), opts, s3c, nil, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "failed listing") {
		t.Errorf("stderr %q missing listing-failure message", stderr.String())
	}
}

func TestSyncUploadFailureReturns1(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeSite(t, dir)

	s3c := &fakeS3{putErr: errors.New("access denied")}
	var stdout, stderr bytes.Buffer
	opts := options{bucket: testBucket, prefix: testPrefix, dir: dir}

	code := sync(context.Background(), opts, s3c, nil, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "upload failed for") {
		t.Errorf("stderr %q missing upload-failure message", stderr.String())
	}
}

func TestSyncDeleteFailureReturns1(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeSite(t, dir)

	s3c := &fakeS3{
		remoteKeys: []string{testPrefix + "old.html"},
		deleteErrors: []types.Error{
			{Key: aws.String(testPrefix + "old.html"), Message: aws.String("AccessDenied")},
		},
	}
	var stdout, stderr bytes.Buffer
	opts := options{bucket: testBucket, prefix: testPrefix, dir: dir}

	code := sync(context.Background(), opts, s3c, nil, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "delete failed") {
		t.Errorf("stderr %q missing delete-failure message", stderr.String())
	}
}

func TestSyncInvalidatesCloudFrontWhenConfigured(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeSite(t, dir)

	s3c := &fakeS3{}
	cf := &fakeCF{}
	var stdout, stderr bytes.Buffer
	opts := options{bucket: testBucket, prefix: testPrefix, dir: dir, cloudfrontID: "EDIST1"}

	code := sync(context.Background(), opts, s3c, cf, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%s", code, stderr.String())
	}
	if cf.captured == nil {
		t.Fatal("CreateInvalidation was not called")
	}
	if aws.ToString(cf.captured.DistributionId) != "EDIST1" {
		t.Errorf("DistributionId = %v, want EDIST1", cf.captured.DistributionId)
	}
	if items := cf.captured.InvalidationBatch.Paths.Items; len(items) != 1 || items[0] != "/*" {
		t.Errorf("unexpected default invalidation paths: %v", items)
	}
	if !strings.Contains(stdout.String(), "invalidating cloudfront distribution EDIST1") {
		t.Errorf("stdout %q missing invalidation announcement", stdout.String())
	}
}

func TestSyncCloudFrontCustomPaths(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeSite(t, dir)

	cf := &fakeCF{}
	var stdout, stderr bytes.Buffer
	opts := options{bucket: testBucket, prefix: testPrefix, dir: dir, cloudfrontID: "EDIST1", cloudfrontPaths: "/a,/b"}

	code := sync(context.Background(), opts, &fakeS3{}, cf, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%s", code, stderr.String())
	}
	items := cf.captured.InvalidationBatch.Paths.Items
	if len(items) != 2 || items[0] != "/a" || items[1] != "/b" {
		t.Errorf("unexpected invalidation paths: %v", items)
	}
}

func TestSyncCloudFrontFailureReturns1(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeSite(t, dir)

	cf := &fakeCF{err: errors.New("cloudfront unavailable")}
	var stdout, stderr bytes.Buffer
	opts := options{bucket: testBucket, prefix: testPrefix, dir: dir, cloudfrontID: "EDIST1"}

	code := sync(context.Background(), opts, &fakeS3{}, cf, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "cloudfront invalidation failed") {
		t.Errorf("stderr %q missing cloudfront-failure message", stderr.String())
	}
}

func TestPrintPlan(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	printPlan(&buf, packer.WebsitePlan{
		Uploads: []packer.WebsiteObject{{Key: "a"}, {Key: "b"}},
		Deletes: []string{"c"},
	}, "my-bucket", "p/", true)

	out := buf.String()
	for _, want := range []string{"dry-run", "my-bucket", "p/", "uploads: 2", "deletes: 1"} {
		if !strings.Contains(out, want) {
			t.Errorf("printPlan output %q missing %q", out, want)
		}
	}
}

func TestWriteLineAndWriteErrorLine(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	writeLine(&buf, "hello")
	if buf.String() != "hello\n" {
		t.Fatalf("writeLine output = %q", buf.String())
	}

	buf.Reset()
	writeErrorLine(&buf, "prefix: ", errors.New("boom"))
	if buf.String() != "prefix: boom\n" {
		t.Fatalf("writeErrorLine output = %q", buf.String())
	}
}

// failingWriter always errors, exercising writeLine's discard-on-write-error
// branch (a broken stdout/stderr pipe should never panic the CLI).
type failingWriter struct{}

func (failingWriter) Write(_ []byte) (int, error) { return 0, errors.New("broken pipe") }

func TestWriteLineSwallowsWriteError(t *testing.T) {
	t.Parallel()

	// Must not panic.
	writeLine(failingWriter{}, "anything")
}

func TestLoadAWSConfigWithRegion(t *testing.T) {
	t.Parallel()

	cfg, err := loadAWSConfig(context.Background(), "us-west-2")
	if err != nil {
		t.Fatalf(testUnexpectedErrorFmt, err)
	}
	if cfg.Region != "us-west-2" {
		t.Fatalf("Region = %q, want us-west-2", cfg.Region)
	}
}

func TestLoadAWSConfigWithoutRegion(t *testing.T) {
	t.Parallel()

	if _, err := loadAWSConfig(context.Background(), ""); err != nil {
		t.Fatalf(testUnexpectedErrorFmt, err)
	}
}

func TestRunReturnsExitCode2OnParseError(t *testing.T) {
	t.Parallel()

	// No AWS calls are reachable here: parseArgs fails before loadAWSConfig
	// or any client is constructed.
	if code := run([]string{"--bucket", testBucket}); code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
}
