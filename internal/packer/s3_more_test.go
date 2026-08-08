package packer

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// batchRecorderS3 records how many keys each DeleteObjects call received, so
// tests can assert DeleteKeys' 1000-key batching boundary without needing a
// real 1000+ object fixture on disk.
type batchRecorderS3 struct {
	batchSizes []int
}

func (b *batchRecorderS3) PutObject(_ context.Context, _ *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	return &s3.PutObjectOutput{}, nil
}

func (b *batchRecorderS3) DeleteObjects(_ context.Context, params *s3.DeleteObjectsInput, _ ...func(*s3.Options)) (*s3.DeleteObjectsOutput, error) {
	if params != nil && params.Delete != nil {
		b.batchSizes = append(b.batchSizes, len(params.Delete.Objects))
	}
	return &s3.DeleteObjectsOutput{}, nil
}

func TestDeleteKeysSplitsIntoBatchesOf1000(t *testing.T) {
	t.Parallel()

	keys := make([]string, 1500)
	for i := range keys {
		keys[i] = "key" + string(rune('a'+i%26))
	}

	rec := &batchRecorderS3{}
	if err := DeleteKeys(context.Background(), rec, "bucket", keys); err != nil {
		t.Fatalf(testUnexpectedErrorFmt, err)
	}
	if len(rec.batchSizes) != 2 {
		t.Fatalf("expected 2 batches, got %d: %v", len(rec.batchSizes), rec.batchSizes)
	}
	if rec.batchSizes[0] != 1000 || rec.batchSizes[1] != 500 {
		t.Fatalf("unexpected batch sizes: %v", rec.batchSizes)
	}
}

type erroringDeleteS3 struct{}

func (e *erroringDeleteS3) PutObject(_ context.Context, _ *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	return &s3.PutObjectOutput{}, nil
}

func (e *erroringDeleteS3) DeleteObjects(_ context.Context, _ *s3.DeleteObjectsInput, _ ...func(*s3.Options)) (*s3.DeleteObjectsOutput, error) {
	return nil, errors.New("boom: delete objects api call failed")
}

func TestDeleteKeysPropagatesAPIError(t *testing.T) {
	t.Parallel()

	err := DeleteKeys(context.Background(), &erroringDeleteS3{}, "bucket", []string{"a"})
	if err == nil {
		t.Fatal(testExpectedErrorGotNil)
	}
}

func TestDeleteKeysNoKeysIsNoop(t *testing.T) {
	t.Parallel()

	rec := &batchRecorderS3{}
	if err := DeleteKeys(context.Background(), rec, "bucket", nil); err != nil {
		t.Fatalf(testUnexpectedErrorFmt, err)
	}
	if len(rec.batchSizes) != 0 {
		t.Fatalf("expected no DeleteObjects calls, got %v", rec.batchSizes)
	}
}

func TestFirstDeleteErrorNilOutput(t *testing.T) {
	t.Parallel()

	if err := firstDeleteError(nil); err != nil {
		t.Fatalf("firstDeleteError(nil) = %v, want nil", err)
	}
}

func TestFirstDeleteErrorSingleNoKeyUsesGenericMessage(t *testing.T) {
	t.Parallel()

	out := &s3.DeleteObjectsOutput{
		Errors: []types.Error{{Message: aws.String("AccessDenied")}},
	}
	err := firstDeleteError(out)
	if err == nil {
		t.Fatal(testExpectedErrorGotNil)
	}
	if !strings.Contains(err.Error(), "AccessDenied") {
		t.Errorf("error %q missing AWS reason", err.Error())
	}
	if strings.Contains(err.Error(), "\"\"") {
		t.Errorf("error %q should not quote an empty key", err.Error())
	}
}

func TestFirstDeleteErrorMultiNoKeyUsesCountAndMessage(t *testing.T) {
	t.Parallel()

	out := &s3.DeleteObjectsOutput{
		Errors: []types.Error{
			{Message: aws.String("InternalError")},
			{Message: aws.String("InternalError")},
		},
	}
	err := firstDeleteError(out)
	if err == nil {
		t.Fatal(testExpectedErrorGotNil)
	}
	if !strings.Contains(err.Error(), "2") {
		t.Errorf("error %q missing failure count", err.Error())
	}
}

func TestFirstDeleteErrorEmptyMessageFallsBackToDefault(t *testing.T) {
	t.Parallel()

	out := &s3.DeleteObjectsOutput{
		Errors: []types.Error{{Key: aws.String("k.html")}},
	}
	err := firstDeleteError(out)
	if err == nil {
		t.Fatal(testExpectedErrorGotNil)
	}
	if !strings.Contains(err.Error(), "delete failed") {
		t.Errorf("error %q missing generic fallback message", err.Error())
	}
}
