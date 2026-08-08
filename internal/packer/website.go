package packer

import (
	"context"
	"errors"
	"fmt"
	"mime"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type WebsiteObject struct {
	Path         string
	Key          string
	ContentType  string
	CacheControl string
}

type WebsitePlan struct {
	Uploads []WebsiteObject
	Deletes []string
}

var hashedAssetToken = regexp.MustCompile(`(?i)[._-][a-f0-9]{8,}[._-]`)

func NormalizePrefix(prefix string) (string, error) {
	p := strings.TrimSpace(prefix)
	if p == "" {
		return "", fmt.Errorf("--prefix is required; use \"/\" to target the bucket root")
	}
	// Strip leading slashes so callers can pass "/sites/dev" or "//sites/dev/"
	// without accidentally creating S3 keys under a leading-slash namespace.
	p = strings.TrimLeft(p, "/")
	if p == "" {
		// Input was "/" or all slashes → bucket root.
		return "", nil
	}
	if !strings.HasSuffix(p, "/") {
		p += "/"
	}
	return p, nil
}

func DiscoverWebsiteObjects(rootDir, prefix string) ([]WebsiteObject, error) {
	var out []WebsiteObject
	if err := requireDir(rootDir); err != nil {
		return nil, err
	}

	if err := filepath.WalkDir(rootDir, func(filePath string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if shouldSkipEntry(d) {
			return nil
		}

		obj, ok, err := websiteObjectFromPath(rootDir, prefix, filePath)
		if err != nil {
			return err
		}
		if !ok {
			return nil
		}
		out = append(out, *obj)
		return nil
	}); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no files found under %q", rootDir)
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}

func requireDir(rootDir string) error {
	info, err := os.Stat(rootDir)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%q is not a directory", rootDir)
	}
	return nil
}

func shouldSkipEntry(d os.DirEntry) bool {
	if d.IsDir() {
		return true
	}
	return !d.Type().IsRegular()
}

func websiteObjectFromPath(rootDir, prefix, filePath string) (*WebsiteObject, bool, error) {
	rel, err := filepath.Rel(rootDir, filePath)
	if err != nil {
		return nil, false, err
	}
	rel = filepath.ToSlash(rel)
	rel = strings.TrimPrefix(rel, "./")
	if rel == "" || rel == "." {
		return nil, false, nil
	}
	if path.Base(rel) == ".DS_Store" {
		return nil, false, nil
	}

	key := prefix + rel
	return &WebsiteObject{
		Path:         filePath,
		Key:          key,
		ContentType:  contentTypeForPath(rel),
		CacheControl: cacheControlForPath(rel),
	}, true, nil
}

func ListRemoteKeys(ctx context.Context, client s3.ListObjectsV2APIClient, bucket, prefix string) (map[string]struct{}, error) {
	out := map[string]struct{}{}

	p := s3.NewListObjectsV2Paginator(client, &s3.ListObjectsV2Input{
		Bucket: aws.String(bucket),
		Prefix: aws.String(prefix),
	})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, obj := range page.Contents {
			if obj.Key == nil {
				continue
			}
			key := *obj.Key
			if strings.HasSuffix(key, "/") {
				continue
			}
			out[key] = struct{}{}
		}
	}
	return out, nil
}

func BuildWebsitePlan(local []WebsiteObject, remote map[string]struct{}, noDelete bool) WebsitePlan {
	desired := map[string]struct{}{}
	for _, o := range local {
		desired[o.Key] = struct{}{}
	}

	var deletes []string
	if !noDelete {
		for key := range remote {
			if _, ok := desired[key]; !ok {
				deletes = append(deletes, key)
			}
		}
		sort.Strings(deletes)
	}

	return WebsitePlan{Uploads: local, Deletes: deletes}
}

func PutWebsiteObject(ctx context.Context, client s3PutDeleteClient, bucket string, o WebsiteObject) error {
	if strings.TrimSpace(o.Key) == "" {
		return errors.New("website object key is empty")
	}
	f, err := os.Open(o.Path)
	if err != nil {
		return err
	}
	defer f.Close()

	input := &s3.PutObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(o.Key),
		Body:   f,
	}
	if strings.TrimSpace(o.ContentType) != "" {
		input.ContentType = aws.String(o.ContentType)
	}
	if strings.TrimSpace(o.CacheControl) != "" {
		input.CacheControl = aws.String(o.CacheControl)
	}
	_, err = client.PutObject(ctx, input)
	return err
}

// scan-fix(coverage:dead-code): the explicit table below is now checked
// BEFORE mime.TypeByExtension, not after. mime.TypeByExtension consults the
// host's /etc/mime.types on Unix in addition to Go's built-in table, and that
// file's contents (or absence) differ between a developer's machine and a CI
// runner. With the old ordering, mime.TypeByExtension already returned a
// non-empty value for nearly every extension listed below (verified: .css,
// .js, .json, .txt, .xml, .html/.htm, .png, .jpg/.jpeg, .gif, .webp, .woff2,
// .woff, .ttf all resolve on a stock Linux install) — so the switch was
// unreachable dead code, and the Content-Type actually served for e.g. a
// .js file silently depended on which machine ran the sync (potentially
// "text/javascript; charset=utf-8" locally vs. a different value in CI),
// rather than the deterministic value this tool intends. Checking the table
// first makes every case here live and the served Content-Type identical
// regardless of environment; mime.TypeByExtension is now only a fallback for
// extensions this tool doesn't explicitly know about.
func contentTypeForPath(rel string) string {
	ext := strings.ToLower(path.Ext(rel))
	switch ext {
	case ".svg":
		return "image/svg+xml"
	case ".webmanifest":
		return "application/manifest+json"
	case ".css":
		return "text/css; charset=utf-8"
	case ".js":
		return "application/javascript; charset=utf-8"
	case ".json":
		return "application/json; charset=utf-8"
	case ".txt":
		return "text/plain; charset=utf-8"
	case ".xml":
		return "application/xml; charset=utf-8"
	case ".html", ".htm":
		return "text/html; charset=utf-8"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".woff2":
		return "font/woff2"
	case ".woff":
		return "font/woff"
	case ".ttf":
		return "font/ttf"
	}
	if ct := mime.TypeByExtension(ext); ct != "" {
		return ct
	}
	return "application/octet-stream"
}

func cacheControlForPath(rel string) string {
	ext := strings.ToLower(path.Ext(rel))
	base := path.Base(rel)

	if ext == ".html" || ext == ".htm" {
		return cacheControlNoCache
	}
	if base == "sitemap.xml" || base == "robots.txt" {
		return cacheControlNoCache
	}
	if hashedAssetToken.MatchString(base) {
		return cacheControlImmutable
	}
	return cacheControlDefault
}
