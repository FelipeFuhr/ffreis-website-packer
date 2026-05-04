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
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/felipefuhr/ffreis-website-packer/internal/packer"
)

type options struct {
	bucket   string
	prefix   string
	dir      string
	region   string
	dryRun   bool
	noDelete bool
}

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	opts, err := parseArgs(args)
	if err != nil {
		_ = writeLine(os.Stderr, err.Error())
		return 2
	}

	ctx := context.Background()

	awsCfg, err := loadAWSConfig(ctx, opts.region)
	if err != nil {
		_ = writeErrorLine(os.Stderr, "failed to load AWS config: ", err)
		return 1
	}

	prefix, err := packer.NormalizePrefix(opts.prefix)
	if err != nil {
		_ = writeLine(os.Stderr, err.Error())
		return 2
	}

	local, err := packer.DiscoverWebsiteObjects(opts.dir, prefix)
	if err != nil {
		_ = writeErrorLine(os.Stderr, "website discovery failed: ", err)
		return 1
	}

	s3Client := s3.NewFromConfig(awsCfg)
	remote, err := packer.ListRemoteKeys(ctx, s3Client, opts.bucket, prefix)
	if err != nil {
		_ = writeErrorLine(os.Stderr, "failed listing s3://"+opts.bucket+"/"+prefix+": ", err)
		return 1
	}

	plan := packer.BuildWebsitePlan(local, remote, opts.noDelete)
	printPlan(plan, opts.bucket, prefix, opts.dryRun)

	if opts.dryRun {
		return 0
	}

	for _, o := range plan.Uploads {
		if err := packer.PutWebsiteObject(ctx, s3Client, opts.bucket, o); err != nil {
			_ = writeErrorLine(os.Stderr, "upload failed for "+o.Key+": ", err)
			return 1
		}
	}
	if err := packer.DeleteKeys(ctx, s3Client, opts.bucket, plan.Deletes); err != nil {
		_ = writeErrorLine(os.Stderr, "delete failed: ", err)
		return 1
	}

	_ = writeLine(os.Stdout, "done: uploaded="+strconv.Itoa(len(plan.Uploads))+" deleted="+strconv.Itoa(len(plan.Deletes)))
	return 0
}

func parseArgs(args []string) (options, error) {
	var opts options
	fs := flag.NewFlagSet("website-packer", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	fs.StringVar(&opts.bucket, "bucket", "", "S3 bucket name (required)")
	fs.StringVar(&opts.prefix, "prefix", "", "S3 key prefix (optional; empty means bucket root, e.g. sites/dev/)")
	fs.StringVar(&opts.dir, "dir", "dist", "Website output dir to sync (recursive)")
	fs.StringVar(&opts.region, "region", "", "AWS region override (optional)")
	fs.BoolVar(&opts.dryRun, "dry-run", false, "Print planned actions without changing S3")
	fs.BoolVar(&opts.noDelete, "no-delete", false, "Upload/update only (do not delete remote extras)")

	if err := fs.Parse(args); err != nil {
		return options{}, err
	}

	if opts.bucket == "" {
		return options{}, fmt.Errorf("--bucket is required")
	}
	return opts, nil
}

func loadAWSConfig(ctx context.Context, region string) (aws.Config, error) {
	if region != "" {
		return config.LoadDefaultConfig(ctx, config.WithRegion(region))
	}
	return config.LoadDefaultConfig(ctx)
}

func printPlan(plan packer.WebsitePlan, bucket, prefix string, dryRun bool) {
	mode := "apply"
	if dryRun {
		mode = "dry-run"
	}
	_ = writeLine(os.Stdout, "website-packer ("+mode+")")
	_ = writeLine(os.Stdout, "bucket: "+bucket)
	_ = writeLine(os.Stdout, "prefix: "+prefix)
	_ = writeLine(os.Stdout, "uploads: "+strconv.Itoa(len(plan.Uploads)))
	_ = writeLine(os.Stdout, "deletes: "+strconv.Itoa(len(plan.Deletes)))
}

func writeLine(w io.Writer, line string) error {
	_, err := io.WriteString(w, line+"\n")
	return err
}

func writeErrorLine(w io.Writer, prefix string, err error) error {
	return writeLine(w, prefix+err.Error())
}
