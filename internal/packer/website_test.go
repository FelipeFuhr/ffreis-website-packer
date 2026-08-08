package packer

import (
	"context"
	"errors"
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

func TestNormalizePrefixAllowsSlashAsRoot(t *testing.T) {
	t.Parallel()

	got, err := NormalizePrefix("/")
	if err != nil {
		t.Fatalf(testUnexpectedErrorFmt, err)
	}
	if got != "" {
		t.Fatalf("NormalizePrefix(\"/\") = %q, want empty string", got)
	}
}

func TestNormalizePrefixRejectsEmpty(t *testing.T) {
	t.Parallel()

	for _, input := range []string{"", "   "} {
		_, err := NormalizePrefix(input)
		if err == nil {
			t.Fatalf("NormalizePrefix(%q): expected error, got nil", input)
		}
	}
}

func TestNormalizePrefixStripsLeadingSlashes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		input string
		want  string
	}{
		{"/sites/dev", "sites/dev/"},
		{"//sites/dev/", "sites/dev/"},
		{"/sites/dev/", "sites/dev/"},
	}
	for _, tc := range cases {
		got, err := NormalizePrefix(tc.input)
		if err != nil {
			t.Fatalf("NormalizePrefix(%q): unexpected error: %v", tc.input, err)
		}
		if got != tc.want {
			t.Fatalf("NormalizePrefix(%q) = %q, want %q", tc.input, got, tc.want)
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

// erroringListS3 fails on the page requested by errOnPage (0-indexed), so
// callers can exercise ListRemoteKeys' mid-pagination error path.
type erroringListS3 struct {
	errOnPage int
	calls     int
}

func (f *erroringListS3) ListObjectsV2(_ context.Context, params *s3.ListObjectsV2Input, _ ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
	page := f.calls
	f.calls++
	if page == f.errOnPage {
		return nil, errors.New("boom: list objects failed")
	}
	return &s3.ListObjectsV2Output{
		Contents:              []types.Object{{Key: strPtr(testRemoteKeyIndexHTML)}},
		IsTruncated:           boolPtr(true),
		NextContinuationToken: strPtr("t1"),
	}, nil
}

func TestListRemoteKeysPropagatesPageError(t *testing.T) {
	t.Parallel()

	s3c := &erroringListS3{errOnPage: 0}
	_, err := ListRemoteKeys(context.Background(), s3c, testBucket, testPrefix)
	if err == nil {
		t.Fatal(testExpectedErrorGotNil)
	}
}

func TestListRemoteKeysSkipsDirectoryMarkers(t *testing.T) {
	t.Parallel()

	// S3 "folder" placeholder objects end in "/" and must not be treated as
	// syncable website files.
	s3c := &staticListS3{
		keys: []string{testRemoteKeyIndexHTML, "p/assets/"},
	}
	remote, err := ListRemoteKeys(context.Background(), s3c, testBucket, testPrefix)
	if err != nil {
		t.Fatalf(testUnexpectedErrorFmt, err)
	}
	if _, ok := remote["p/assets/"]; ok {
		t.Fatal("directory-marker key p/assets/ should have been skipped")
	}
	if _, ok := remote[testRemoteKeyIndexHTML]; !ok {
		t.Fatal("expected regular key to be present")
	}
}

func TestListRemoteKeysSkipsNilKeys(t *testing.T) {
	t.Parallel()

	s3c := &nilKeyListS3{}
	remote, err := ListRemoteKeys(context.Background(), s3c, testBucket, testPrefix)
	if err != nil {
		t.Fatalf(testUnexpectedErrorFmt, err)
	}
	if len(remote) != 0 {
		t.Fatalf("expected no keys, got %v", remote)
	}
}

type staticListS3 struct{ keys []string }

func (f *staticListS3) ListObjectsV2(_ context.Context, _ *s3.ListObjectsV2Input, _ ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
	var contents []types.Object
	for _, k := range f.keys {
		contents = append(contents, types.Object{Key: strPtr(k)})
	}
	return &s3.ListObjectsV2Output{Contents: contents, IsTruncated: boolPtr(false)}, nil
}

type nilKeyListS3 struct{}

func (f *nilKeyListS3) ListObjectsV2(_ context.Context, _ *s3.ListObjectsV2Input, _ ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
	return &s3.ListObjectsV2Output{Contents: []types.Object{{Key: nil}}, IsTruncated: boolPtr(false)}, nil
}

func TestDiscoverWebsiteObjectsMissingDir(t *testing.T) {
	t.Parallel()

	_, err := DiscoverWebsiteObjects(filepath.Join(t.TempDir(), "does-not-exist"), testSitePrefix)
	if err == nil {
		t.Fatal(testExpectedErrorGotNil)
	}
}

func TestDiscoverWebsiteObjectsRejectsFileArg(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := filepath.Join(dir, "not-a-dir")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := DiscoverWebsiteObjects(file, testSitePrefix)
	if err == nil {
		t.Fatal(testExpectedErrorGotNil)
	}
}

func TestDiscoverWebsiteObjectsEmptyDirErrors(t *testing.T) {
	t.Parallel()

	_, err := DiscoverWebsiteObjects(t.TempDir(), testSitePrefix)
	if err == nil {
		t.Fatal(testExpectedErrorGotNil)
	}
}

func TestDiscoverWebsiteObjectsSkipsDSStoreAndDirs(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".DS_Store"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub", "keep.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	objs, err := DiscoverWebsiteObjects(dir, testSitePrefix)
	if err != nil {
		t.Fatalf(testUnexpectedErrorFmt, err)
	}
	if len(objs) != 1 || objs[0].Key != testSitePrefix+"sub/keep.txt" {
		t.Fatalf("unexpected objects: %+v", objs)
	}
}

func TestPutWebsiteObjectRejectsEmptyKey(t *testing.T) {
	t.Parallel()

	s3c := &fakeS3{}
	err := PutWebsiteObject(context.Background(), s3c, testBucket, WebsiteObject{Path: "irrelevant", Key: "  "})
	if err == nil {
		t.Fatal(testExpectedErrorGotNil)
	}
	if len(s3c.putCalls) != 0 {
		t.Fatal("PutObject should not be called for an empty key")
	}
}

func TestPutWebsiteObjectMissingFile(t *testing.T) {
	t.Parallel()

	s3c := &fakeS3{}
	err := PutWebsiteObject(context.Background(), s3c, testBucket, WebsiteObject{
		Path: filepath.Join(t.TempDir(), "missing.txt"),
		Key:  "sites/dev/missing.txt",
	})
	if err == nil {
		t.Fatal(testExpectedErrorGotNil)
	}
}

func TestPutWebsiteObjectUploadsWithContentTypeAndCacheControl(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "app.js")
	if err := os.WriteFile(path, []byte("console.log(1)"), 0o644); err != nil {
		t.Fatal(err)
	}

	s3c := &fakeS3{}
	obj := WebsiteObject{
		Path:         path,
		Key:          "sites/dev/app.js",
		ContentType:  "application/javascript; charset=utf-8",
		CacheControl: cacheControlDefault,
	}
	if err := PutWebsiteObject(context.Background(), s3c, testBucket, obj); err != nil {
		t.Fatalf(testUnexpectedErrorFmt, err)
	}
	if len(s3c.putCalls) != 1 {
		t.Fatalf("PutObject calls = %d, want 1", len(s3c.putCalls))
	}
	put := s3c.putCalls[0]
	if put.Key == nil || *put.Key != obj.Key {
		t.Fatalf("unexpected Key: %v", put.Key)
	}
	if put.ContentType == nil || *put.ContentType != obj.ContentType {
		t.Fatalf("unexpected ContentType: %v", put.ContentType)
	}
	if put.CacheControl == nil || *put.CacheControl != obj.CacheControl {
		t.Fatalf("unexpected CacheControl: %v", put.CacheControl)
	}
}

func TestContentTypeForPath(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"a.svg":         "image/svg+xml",
		"a.webmanifest": "application/manifest+json",
		"a.css":         "text/css; charset=utf-8",
		"a.js":          "application/javascript; charset=utf-8",
		"a.json":        "application/json; charset=utf-8",
		"a.txt":         "text/plain; charset=utf-8",
		"a.xml":         "application/xml; charset=utf-8",
		"a.html":        "text/html; charset=utf-8",
		"a.htm":         "text/html; charset=utf-8",
		"a.png":         "image/png",
		"a.jpg":         "image/jpeg",
		"a.jpeg":        "image/jpeg",
		"a.gif":         "image/gif",
		"a.webp":        "image/webp",
		"a.woff2":       "font/woff2",
		"a.woff":        "font/woff",
		"a.ttf":         "font/ttf",
		"a.unknownext":  "application/octet-stream",
	}
	for rel, want := range cases {
		if got := contentTypeForPath(rel); got != want {
			t.Errorf("contentTypeForPath(%q) = %q, want %q", rel, got, want)
		}
	}
}

func TestCacheControlForPath(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"index.html":       cacheControlNoCache,
		"index.htm":        cacheControlNoCache,
		"sitemap.xml":      cacheControlNoCache,
		"robots.txt":       cacheControlNoCache,
		"app.abcdef01.css": cacheControlImmutable,
		"app.js":           cacheControlDefault,
		"favicon.ico":      cacheControlDefault,
	}
	for rel, want := range cases {
		if got := cacheControlForPath(rel); got != want {
			t.Errorf("cacheControlForPath(%q) = %q, want %q", rel, got, want)
		}
	}
}
