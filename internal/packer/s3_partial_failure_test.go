package packer

import (
	"context"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// partialFailureS3 returns a DeleteObjects response with the configured Errors
// list, simulating an S3 batch where some keys deleted and others didn't.
type partialFailureS3 struct {
	errs []types.Error
}

func (p *partialFailureS3) PutObject(_ context.Context, _ *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	return &s3.PutObjectOutput{}, nil
}

func (p *partialFailureS3) DeleteObjects(_ context.Context, _ *s3.DeleteObjectsInput, _ ...func(*s3.Options)) (*s3.DeleteObjectsOutput, error) {
	return &s3.DeleteObjectsOutput{Errors: p.errs}, nil
}

// TestDeleteKeys_SurfaceSingleFailure pins the single-error message format:
// it must name the key and the AWS-supplied reason.
func TestDeleteKeysSurfaceSingleFailure(t *testing.T) {
	fake := &partialFailureS3{
		errs: []types.Error{
			{Key: aws.String("locked.html"), Message: aws.String("AccessDenied")},
		},
	}

	err := DeleteKeys(context.Background(), fake, "bucket", []string{"locked.html"})
	if err == nil {
		t.Fatal("DeleteKeys returned nil despite Errors response")
	}
	msg := err.Error()
	if !strings.Contains(msg, "locked.html") {
		t.Errorf("error %q does not mention the failing key", msg)
	}
	if !strings.Contains(msg, "AccessDenied") {
		t.Errorf("error %q does not include the AWS reason", msg)
	}
}

// TestDeleteKeys_SurfacesPartialFailureCount is the regression guard for the
// prior behaviour where firstDeleteError returned just the first error's
// message — making a 17-key partial failure look like a single bad key in
// CI logs. The fixed version reports the total count.
func TestDeleteKeysSurfacesPartialFailureCount(t *testing.T) {
	errs := []types.Error{
		{Key: aws.String("a/b/c.html"), Message: aws.String("InternalError")},
		{Key: aws.String("a/b/d.html"), Message: aws.String("InternalError")},
		{Key: aws.String("a/b/e.html"), Message: aws.String("InternalError")},
	}
	fake := &partialFailureS3{errs: errs}

	err := DeleteKeys(context.Background(), fake, "bucket", []string{"a/b/c.html", "a/b/d.html", "a/b/e.html"})
	if err == nil {
		t.Fatal("expected error for partial-failure DeleteObjects response")
	}
	msg := err.Error()
	if !strings.Contains(msg, "3") {
		t.Errorf("error %q does not include the failure count (3)", msg)
	}
	if !strings.Contains(msg, "a/b/c.html") {
		t.Errorf("error %q does not mention the first failing key", msg)
	}
	// Defense in depth: ensure we don't accidentally regress to printing the
	// same message twice (the prior format string did "%w: %s" with the same
	// value, leading to "AccessDenied: AccessDenied (key)" duplication).
	first := strings.Index(msg, "InternalError")
	if first >= 0 {
		if strings.Contains(msg[first+len("InternalError"):], "InternalError") {
			t.Errorf("error includes duplicated reason text: %q", msg)
		}
	}
}

// TestDeleteKeys_EmptyErrorsIsSuccess confirms the documented contract: an
// AWS DeleteObjects response with Errors=[] means every key was deleted, so
// DeleteKeys returns nil (not an error).
func TestDeleteKeysEmptyErrorsIsSuccess(t *testing.T) {
	fake := &partialFailureS3{errs: nil}
	if err := DeleteKeys(context.Background(), fake, "bucket", []string{"k1", "k2"}); err != nil {
		t.Errorf("DeleteKeys with empty Errors returned %v, want nil", err)
	}
}
