// Package main provides the `website-packer` CLI.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/cloudfront"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/felipefuhr/ffreis-website-packer/internal/packer"
)

type options struct {
	bucket          string
	prefix          string
	dir             string
	region          string
	dryRun          bool
	noDelete        bool
	cloudfrontID    string
	cloudfrontPaths string
}

// s3SyncClient is satisfied by *s3.Client. Declaring it locally (rather than
// importing an unexported type from internal/packer) lets tests inject a
// fake here without touching real AWS — mirroring the interface-based mock
// convention already used throughout internal/packer.
type s3SyncClient interface {
	ListObjectsV2(ctx context.Context, params *s3.ListObjectsV2Input, optFns ...func(*s3.Options)) (*s3.ListObjectsV2Output, error)
	PutObject(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	DeleteObjects(ctx context.Context, params *s3.DeleteObjectsInput, optFns ...func(*s3.Options)) (*s3.DeleteObjectsOutput, error)
}

// cfInvalidator is satisfied by *cloudfront.Client; declared locally for the
// same test-injection reason as s3SyncClient.
type cfInvalidator interface {
	CreateInvalidation(ctx context.Context, params *cloudfront.CreateInvalidationInput, optFns ...func(*cloudfront.Options)) (*cloudfront.CreateInvalidationOutput, error)
}

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	opts, err := parseArgs(args)
	if err != nil {
		writeLine(os.Stderr, err.Error())
		return 2
	}

	ctx := context.Background()

	awsCfg, err := loadAWSConfig(ctx, opts.region)
	if err != nil {
		writeErrorLine(os.Stderr, "failed to load AWS config: ", err)
		return 1
	}

	s3Client := s3.NewFromConfig(awsCfg)
	var cfClient cfInvalidator
	if opts.cloudfrontID != "" {
		cfClient = cloudfront.NewFromConfig(awsCfg)
	}

	return sync(ctx, opts, s3Client, cfClient, os.Stdout, os.Stderr)
}

// sync runs the actual discover → plan → upload/delete → invalidate pipeline
// against already-constructed clients, so tests can exercise every branch
// with fakes instead of hitting real AWS (see main_sync_test.go).
func sync(ctx context.Context, opts options, s3Client s3SyncClient, cfClient cfInvalidator, stdout, stderr io.Writer) int {
	prefix, err := packer.NormalizePrefix(opts.prefix)
	if err != nil {
		writeLine(stderr, err.Error())
		return 2
	}

	local, err := packer.DiscoverWebsiteObjects(opts.dir, prefix)
	if err != nil {
		writeErrorLine(stderr, "website discovery failed: ", err)
		return 1
	}

	remote, err := packer.ListRemoteKeys(ctx, s3Client, opts.bucket, prefix)
	if err != nil {
		writeErrorLine(stderr, "failed listing s3://"+opts.bucket+"/"+prefix+": ", err)
		return 1
	}

	plan := packer.BuildWebsitePlan(local, remote, opts.noDelete)
	printPlan(stdout, plan, opts.bucket, prefix, opts.dryRun)

	if opts.dryRun {
		return 0
	}

	for _, o := range plan.Uploads {
		if err := packer.PutWebsiteObject(ctx, s3Client, opts.bucket, o); err != nil {
			writeErrorLine(stderr, "upload failed for "+o.Key+": ", err)
			return 1
		}
	}
	if err := packer.DeleteKeys(ctx, s3Client, opts.bucket, plan.Deletes); err != nil {
		writeErrorLine(stderr, "delete failed: ", err)
		return 1
	}

	if opts.cloudfrontID != "" {
		paths := opts.cloudfrontPaths
		if paths == "" {
			paths = "/*"
		}
		writeLine(stdout, "invalidating cloudfront distribution "+opts.cloudfrontID+" paths="+paths)
		if err := packer.InvalidateDistribution(ctx, cfClient, opts.cloudfrontID, paths); err != nil {
			writeErrorLine(stderr, "cloudfront invalidation failed: ", err)
			return 1
		}
	}

	writeLine(stdout, "done: uploaded="+strconv.Itoa(len(plan.Uploads))+" deleted="+strconv.Itoa(len(plan.Deletes)))
	return 0
}

func parseArgs(args []string) (options, error) {
	var opts options
	fs := flag.NewFlagSet("website-packer", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	fs.StringVar(&opts.bucket, "bucket", "", "S3 bucket name (required)")
	fs.StringVar(&opts.prefix, "prefix", "", "S3 key prefix (required; use \"/\" to target the bucket root, e.g. sites/dev/)")
	fs.StringVar(&opts.dir, "dir", "dist", "Website output dir to sync (recursive)")
	fs.StringVar(&opts.region, "region", "", "AWS region override (optional)")
	fs.BoolVar(&opts.dryRun, "dry-run", false, "Print planned actions without changing S3")
	fs.BoolVar(&opts.noDelete, "no-delete", false, "Upload/update only (do not delete remote extras)")
	fs.StringVar(&opts.cloudfrontID, "cloudfront-id", "", "CloudFront distribution ID to invalidate after sync (optional)")
	fs.StringVar(&opts.cloudfrontPaths, "cloudfront-paths", "", "Comma-separated invalidation paths (default \"/*\"; only used when --cloudfront-id is set)")

	if err := fs.Parse(args); err != nil {
		return options{}, err
	}

	if opts.bucket == "" {
		return options{}, fmt.Errorf("--bucket is required")
	}
	if opts.prefix == "" {
		return options{}, fmt.Errorf("--prefix is required; use \"/\" to target the bucket root")
	}
	return opts, nil
}

func loadAWSConfig(ctx context.Context, region string) (aws.Config, error) {
	if region != "" {
		return config.LoadDefaultConfig(ctx, config.WithRegion(region))
	}
	return config.LoadDefaultConfig(ctx)
}

func printPlan(w io.Writer, plan packer.WebsitePlan, bucket, prefix string, dryRun bool) {
	mode := "apply"
	if dryRun {
		mode = "dry-run"
	}
	writeLine(w, "website-packer ("+mode+")")
	writeLine(w, "bucket: "+bucket)
	writeLine(w, "prefix: "+prefix)
	writeLine(w, "uploads: "+strconv.Itoa(len(plan.Uploads)))
	writeLine(w, "deletes: "+strconv.Itoa(len(plan.Deletes)))
}

func writeLine(w io.Writer, line string) {
	// Write failures to CLI output are not actionable; discard silently.
	if _, err := io.WriteString(w, line+"\n"); err != nil {
		return
	}
}

func writeErrorLine(w io.Writer, prefix string, err error) {
	writeLine(w, prefix+err.Error())
}
