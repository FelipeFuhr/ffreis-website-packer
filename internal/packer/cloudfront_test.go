package packer

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudfront"
)

// stubCF records the last CreateInvalidation call and optionally returns an error.
type stubCF struct {
	err      error
	captured *cloudfront.CreateInvalidationInput
}

func (s *stubCF) CreateInvalidation(_ context.Context, params *cloudfront.CreateInvalidationInput, _ ...func(*cloudfront.Options)) (*cloudfront.CreateInvalidationOutput, error) {
	s.captured = params
	return &cloudfront.CreateInvalidationOutput{}, s.err
}

// TestInvalidateDistribution_ForwardsDistributionID confirms the distribution
// ID is passed verbatim to CreateInvalidation.
func TestInvalidateDistribution_ForwardsDistributionID(t *testing.T) {
	stub := &stubCF{}
	if err := InvalidateDistribution(context.Background(), stub, "EDIST123", "/*"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := aws.ToString(stub.captured.DistributionId)
	if got != "EDIST123" {
		t.Errorf("DistributionId = %q, want %q", got, "EDIST123")
	}
}

// TestInvalidateDistribution_SinglePath verifies a single-path invalidation
// sends exactly one item with the correct pattern.
func TestInvalidateDistribution_SinglePath(t *testing.T) {
	stub := &stubCF{}
	if err := InvalidateDistribution(context.Background(), stub, "EDIST123", "/*"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	items := stub.captured.InvalidationBatch.Paths.Items
	qty := aws.ToInt32(stub.captured.InvalidationBatch.Paths.Quantity)
	if len(items) != 1 || items[0] != "/*" {
		t.Errorf("unexpected path items: %v", items)
	}
	if qty != 1 {
		t.Errorf("Quantity = %d, want 1", qty)
	}
}

// TestInvalidateDistribution_MultiPath confirms comma-separated paths are split
// into individual items, with leading/trailing whitespace trimmed.
func TestInvalidateDistribution_MultiPath(t *testing.T) {
	stub := &stubCF{}
	if err := InvalidateDistribution(context.Background(), stub, "EDIST123", "/css/*, /js/*, /index.html"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	items := stub.captured.InvalidationBatch.Paths.Items
	qty := aws.ToInt32(stub.captured.InvalidationBatch.Paths.Quantity)
	if len(items) != 3 {
		t.Errorf("expected 3 path items, got %d: %v", len(items), items)
	}
	if qty != int32(len(items)) {
		t.Errorf("Quantity = %d, want %d (must match len(Items))", qty, len(items))
	}
	want := []string{"/css/*", "/js/*", "/index.html"}
	for i, w := range want {
		if items[i] != w {
			t.Errorf("items[%d] = %q, want %q", i, items[i], w)
		}
	}
}

// TestInvalidateDistribution_PropagatesError confirms API errors are surfaced.
func TestInvalidateDistribution_PropagatesError(t *testing.T) {
	boom := errors.New("cloudfront unavailable")
	stub := &stubCF{err: boom}
	err := InvalidateDistribution(context.Background(), stub, "EDIST123", "/*")
	if !errors.Is(err, boom) {
		t.Errorf("got %v, want %v", err, boom)
	}
}

// TestInvalidateDistribution_EmptyPaths verifies that an empty path string
// returns an error without calling the API.
func TestInvalidateDistribution_EmptyPaths(t *testing.T) {
	stub := &stubCF{}
	err := InvalidateDistribution(context.Background(), stub, "EDIST123", "")
	if err == nil {
		t.Error("expected error for empty paths, got nil")
	}
	if stub.captured != nil {
		t.Error("CreateInvalidation should not be called for empty paths")
	}
}

// TestInvalidateDistribution_WhitespaceOnlyPaths verifies that all-whitespace
// paths are treated as empty (no valid pattern).
func TestInvalidateDistribution_WhitespaceOnlyPaths(t *testing.T) {
	stub := &stubCF{}
	err := InvalidateDistribution(context.Background(), stub, "EDIST123", "  ,  ,  ")
	if err == nil {
		t.Error("expected error for whitespace-only paths, got nil")
	}
}

// TestSplitPaths covers the splitting helper directly.
func TestSplitPaths(t *testing.T) {
	cases := []struct {
		input string
		want  []string
	}{
		{"/*", []string{"/*"}},
		{"/a, /b, /c", []string{"/a", "/b", "/c"}},
		{"", nil},
		{"  ,  ", nil},
		{" /x/* , /y ", []string{"/x/*", "/y"}},
	}
	for _, c := range cases {
		got := splitPaths(c.input)
		if len(got) != len(c.want) {
			t.Errorf("splitPaths(%q) = %v, want %v", c.input, got, c.want)
			continue
		}
		for i := range c.want {
			if got[i] != c.want[i] {
				t.Errorf("splitPaths(%q)[%d] = %q, want %q", c.input, i, got[i], c.want[i])
			}
		}
	}
}
