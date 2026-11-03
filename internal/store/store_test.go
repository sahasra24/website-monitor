package store

import (
	"context"
	"testing"
	"time"
)

func TestSiteChecksAndSummary(t *testing.T) {
	database, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	ctx := context.Background()
	site, err := database.AddSite(ctx, "Example", "https://example.com")
	if err != nil {
		t.Fatal(err)
	}
	checkedAt := time.Now().UTC().Truncate(time.Microsecond)
	if err := database.RecordCheck(ctx, Check{
		SiteID: site.ID, CheckedAt: checkedAt, StatusCode: 200,
		ResponseTime: 125 * time.Millisecond, Available: true,
	}); err != nil {
		t.Fatal(err)
	}

	summaries, err := database.Summaries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 1 {
		t.Fatalf("got %d summaries, want 1", len(summaries))
	}
	summary := summaries[0]
	if summary.TotalChecks != 1 || summary.Successful != 1 || summary.UptimePercent != 100 || !summary.IsUp {
		t.Fatalf("unexpected summary: %+v", summary)
	}
	if summary.LastStatus != 200 || !summary.LastCheckedAt.Equal(checkedAt) {
		t.Fatalf("unexpected latest check fields: %+v", summary)
	}

	history, err := database.History(ctx, site.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 || history[0].ResponseTime != 125*time.Millisecond {
		t.Fatalf("unexpected history: %+v", history)
	}
}

func TestAddSiteRequiresHTTPURL(t *testing.T) {
	database, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	if _, err := database.AddSite(context.Background(), "Invalid", "ftp://example.com"); err == nil {
		t.Fatal("expected invalid URL to be rejected")
	}
}
