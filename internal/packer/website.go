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
	if p == "/" {
		return "", nil
	}
	p = strings.TrimPrefix(p, "/")
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

func contentTypeForPath(rel string) string {
	ext := strings.ToLower(path.Ext(rel))
	switch ext {
	case ".svg":
		return "image/svg+xml"
	case ".webmanifest":
		return "application/manifest+json"
	}
	ct := mime.TypeByExtension(ext)
	if ct != "" {
		return ct
	}
	switch ext {
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
	default:
		return "application/octet-stream"
	}
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
