package monitor

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"website-monitor/internal/store"
)

func TestCheckAllRespectsWorkerLimit(t *testing.T) {
	var active atomic.Int32
	var maximum atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		current := active.Add(1)
		for observed := maximum.Load(); current > observed; observed = maximum.Load() {
			if maximum.CompareAndSwap(observed, current) {
				break
			}
		}
		time.Sleep(25 * time.Millisecond)
		active.Add(-1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	database := openTestStore(t)
	for i := 0; i < 6; i++ {
		if _, err := database.AddSite(context.Background(), "", server.URL+"/"+string(rune('a'+i))); err != nil {
			t.Fatal(err)
		}
	}

	New(database, Config{Timeout: time.Second, Workers: 2}, testLogger()).CheckAll(context.Background())
	if got := maximum.Load(); got != 2 {
		t.Fatalf("maximum concurrent requests = %d, want 2", got)
	}
	summaries, err := database.Summaries(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 6 {
		t.Fatalf("got %d site summaries, want 6", len(summaries))
	}
}

func TestCheckRetriesFailure(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if requests.Add(1) == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	database := openTestStore(t)
	if _, err := database.AddSite(context.Background(), "Retry", server.URL); err != nil {
		t.Fatal(err)
	}
	New(database, Config{Timeout: time.Second, Workers: 1, Retries: 1}, testLogger()).CheckAll(context.Background())

	if got := requests.Load(); got != 2 {
		t.Fatalf("request count = %d, want 2", got)
	}
	summaries, err := database.Summaries(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 1 || !summaries[0].IsUp {
		t.Fatalf("retry did not record successful check: %+v", summaries)
	}
}

func openTestStore(t *testing.T) *store.Store {
	t.Helper()
	database, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Error(err)
		}
	})
	return database
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
