package packer

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

type fakeS3 struct {
	putCalls    []s3.PutObjectInput
	deleteCalls [][]string

	listCalls []*s3.ListObjectsV2Input
}

func (f *fakeS3) PutObject(_ context.Context, params *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	if params != nil {
		f.putCalls = append(f.putCalls, *params)
	}
	return &s3.PutObjectOutput{}, nil
}

func (f *fakeS3) DeleteObjects(_ context.Context, params *s3.DeleteObjectsInput, _ ...func(*s3.Options)) (*s3.DeleteObjectsOutput, error) {
	var keys []string
	if params != nil && params.Delete != nil {
		for _, obj := range params.Delete.Objects {
			if obj.Key != nil {
				keys = append(keys, *obj.Key)
			}
		}
	}
	f.deleteCalls = append(f.deleteCalls, keys)
	return &s3.DeleteObjectsOutput{}, nil
}

func (f *fakeS3) ListObjectsV2(_ context.Context, params *s3.ListObjectsV2Input, _ ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
	f.listCalls = append(f.listCalls, params)
	if params == nil || params.ContinuationToken == nil {
		return &s3.ListObjectsV2Output{
			Contents: []types.Object{
				{Key: strPtr(testRemoteKeyIndexHTML)},
				{Key: strPtr(testRemoteKeyAppJS)},
			},
			IsTruncated:           boolPtr(true),
			NextContinuationToken: strPtr("t1"),
		}, nil
	}
	return &s3.ListObjectsV2Output{
		Contents: []types.Object{
			{Key: strPtr(testRemoteKeyOldJS)},
		},
		IsTruncated: boolPtr(false),
	}, nil
}

func strPtr(s string) *string { return &s }
func boolPtr(b bool) *bool    { return &b }

func TestNormalizePrefix(t *testing.T) {
	t.Parallel()

	got, err := NormalizePrefix("sites/dev")
	if err != nil {
		t.Fatalf(testUnexpectedErrorFmt, err)
	}
	if got != testSitePrefix {
		t.Fatalf("got %q, want %q", got, testSitePrefix)
	}
}

func TestNormalizePrefixAllowsBucketRoot(t *testing.T) {
	t.Parallel()

	for _, input := range []string{"", "/", "   "} {
		got, err := NormalizePrefix(input)
		if err != nil {
			t.Fatalf(testUnexpectedErrorFmt, err)
		}
		if got != "" {
			t.Fatalf("NormalizePrefix(%q) = %q, want empty string", input, got)
		}
	}
}

func TestDiscoverWebsiteObjectsBuildsKeys(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "css"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html></html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "css", "app.abcdef012345.css"), []byte("body{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	objs, err := DiscoverWebsiteObjects(dir, testSitePrefix)
	if err != nil {
		t.Fatalf(testUnexpectedErrorFmt, err)
	}
	if len(objs) != 2 {
		t.Fatalf("got %d objects, want 2", len(objs))
	}

	var keys []string
	for _, o := range objs {
		keys = append(keys, o.Key)
	}
	sort.Strings(keys)
	if keys[0] != "sites/dev/css/app.abcdef012345.css" || keys[1] != "sites/dev/index.html" {
		t.Fatalf("unexpected keys: %v", keys)
	}

	var htmlObj, cssObj WebsiteObject
	for _, o := range objs {
		if filepath.Base(o.Path) == "index.html" {
			htmlObj = o
		} else {
			cssObj = o
		}
	}
	if htmlObj.CacheControl != cacheControlNoCache {
		t.Fatalf("html CacheControl=%q, want %q", htmlObj.CacheControl, cacheControlNoCache)
	}
	if cssObj.CacheControl != cacheControlImmutable {
		t.Fatalf("css CacheControl=%q, want immutable", cssObj.CacheControl)
	}
}

func TestBuildWebsitePlanDeletesExtras(t *testing.T) {
	t.Parallel()

	local := []WebsiteObject{
		{Key: testRemoteKeyIndexHTML},
		{Key: testRemoteKeyAppJS},
	}
	remote := map[string]struct{}{
		testRemoteKeyIndexHTML: {},
		testRemoteKeyAppJS:     {},
		"p/assets/old.js":      {},
		"p/assets/old.css":     {},
	}

	plan := BuildWebsitePlan(local, remote, false)
	if len(plan.Uploads) != 2 {
		t.Fatalf("uploads=%d, want 2", len(plan.Uploads))
	}
	if len(plan.Deletes) != 2 {
		t.Fatalf("deletes=%d, want 2", len(plan.Deletes))
	}
}

func TestListRemoteKeysPaginates(t *testing.T) {
	t.Parallel()

	s3c := &fakeS3{}
	remote, err := ListRemoteKeys(context.Background(), s3c, testBucket, testPrefix)
	if err != nil {
		t.Fatalf(testUnexpectedErrorFmt, err)
	}

	var keys []string
	for k := range remote {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if len(keys) != 3 || keys[0] != testRemoteKeyAppJS || keys[1] != testRemoteKeyIndexHTML || keys[2] != testRemoteKeyOldJS {
		t.Fatalf("unexpected remote keys: %v", keys)
	}
}
