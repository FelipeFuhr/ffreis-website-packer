package packer

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudfront"
	"github.com/aws/aws-sdk-go-v2/service/cloudfront/types"
)

// cfInvalidateClient is satisfied by cloudfront.Client and allows interface-based testing.
type cfInvalidateClient interface {
	CreateInvalidation(ctx context.Context, params *cloudfront.CreateInvalidationInput, optFns ...func(*cloudfront.Options)) (*cloudfront.CreateInvalidationOutput, error)
}

// InvalidateDistribution creates a CloudFront invalidation for distributionID
// with the given comma-separated path patterns (e.g. "/*" or "/css/*,/js/*").
// It fires the invalidation and returns without waiting for completion.
func InvalidateDistribution(ctx context.Context, client cfInvalidateClient, distributionID, paths string) error {
	items := splitPaths(paths)
	if len(items) == 0 {
		return fmt.Errorf("cloudfront invalidation requires at least one path pattern")
	}

	// CallerReference must be unique per invalidation; timestamp at millisecond
	// resolution is sufficient — two syncs within 1ms of each other is not a
	// real-world concern for this use case.
	caller := fmt.Sprintf("website-packer-%d", time.Now().UnixMilli())

	_, err := client.CreateInvalidation(ctx, &cloudfront.CreateInvalidationInput{
		DistributionId: aws.String(distributionID),
		InvalidationBatch: &types.InvalidationBatch{
			CallerReference: aws.String(caller),
			Paths: &types.Paths{
				Quantity: aws.Int32(int32(len(items))),
				Items:    items,
			},
		},
	})
	return err
}

func splitPaths(paths string) []string {
	var out []string
	for _, p := range strings.Split(paths, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
