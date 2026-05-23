package packer

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

type s3PutDeleteClient interface {
	PutObject(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	DeleteObjects(ctx context.Context, params *s3.DeleteObjectsInput, optFns ...func(*s3.Options)) (*s3.DeleteObjectsOutput, error)
}

func DeleteKeys(ctx context.Context, client s3PutDeleteClient, bucket string, keys []string) error {
	const batchSize = 1000
	for start := 0; start < len(keys); start += batchSize {
		end := start + batchSize
		if end > len(keys) {
			end = len(keys)
		}
		if err := deleteKeyBatch(ctx, client, bucket, keys[start:end]); err != nil {
			return err
		}
	}
	return nil
}

func deleteKeyBatch(ctx context.Context, client s3PutDeleteClient, bucket string, keys []string) error {
	out, err := client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
		Bucket: aws.String(bucket),
		Delete: &types.Delete{Objects: objectIdentifiers(keys), Quiet: aws.Bool(true)},
	})
	if err != nil {
		return err
	}
	return firstDeleteError(out)
}

func objectIdentifiers(keys []string) []types.ObjectIdentifier {
	objs := make([]types.ObjectIdentifier, 0, len(keys))
	for _, key := range keys {
		k := key
		objs = append(objs, types.ObjectIdentifier{Key: aws.String(k)})
	}
	return objs
}

// firstDeleteError summarises a DeleteObjects response that reported one or
// more per-object failures. It returns the first failure's key+message and
// also the total failure count, so callers don't silently treat a 5-key
// partial failure as a single-key issue.
//
// DeleteObjects responses with Errors=[] (or out=nil) indicate every object
// in the batch was deleted; in that case nil is returned.
func firstDeleteError(out *s3.DeleteObjectsOutput) error {
	if out == nil || len(out.Errors) == 0 {
		return nil
	}

	first := out.Errors[0]
	key := aws.ToString(first.Key)
	msg := aws.ToString(first.Message)
	if msg == "" {
		msg = "delete failed"
	}

	total := len(out.Errors)
	switch {
	case total == 1 && key != "":
		return fmt.Errorf("delete failed for %q: %s", key, msg)
	case total == 1:
		return fmt.Errorf("delete failed: %s", msg)
	case key != "":
		return fmt.Errorf("delete failed for %d objects (first: %q: %s)", total, key, msg)
	default:
		return fmt.Errorf("delete failed for %d objects (first: %s)", total, msg)
	}
}
