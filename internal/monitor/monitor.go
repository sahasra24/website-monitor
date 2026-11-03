package monitor

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"website-monitor/internal/store"
)

type Config struct {
	Interval time.Duration
	Timeout  time.Duration
	Workers  int
	Retries  int
}

type Monitor struct {
	store  *store.Store
	config Config
	logger *slog.Logger
	client *http.Client
}

func New(database *store.Store, config Config, logger *slog.Logger) *Monitor {
	return &Monitor{
		store: database, config: config, logger: logger,
		client: &http.Client{Timeout: config.Timeout},
	}
}

func (m *Monitor) Run(ctx context.Context) {
	m.CheckAll(ctx)
	ticker := time.NewTicker(m.config.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.CheckAll(ctx)
		}
	}
}

func (m *Monitor) CheckAll(ctx context.Context) {
	sites, err := m.store.Sites(ctx)
	if err != nil {
		m.logger.Error("load sites for check", "error", err)
		return
	}
	if len(sites) == 0 {
		return
	}

	workers := m.config.Workers
	if workers > len(sites) {
		workers = len(sites)
	}
	jobs := make(chan store.Site)
	var group sync.WaitGroup
	for range workers {
		group.Add(1)
		go func() {
			defer group.Done()
			for site := range jobs {
				m.checkSite(ctx, site)
			}
		}()
	}

sendJobs:
	for _, site := range sites {
		select {
		case <-ctx.Done():
			break sendJobs
		case jobs <- site:
		}
	}
	close(jobs)
	group.Wait()
}

func (m *Monitor) checkSite(ctx context.Context, site store.Site) {
	previous, err := m.store.LatestCheck(ctx, site.ID)
	if err != nil {
		m.logger.Error("load previous check", "site", site.URL, "error", err)
	}

	check := store.Check{SiteID: site.ID, CheckedAt: time.Now().UTC()}
	for attempt := 0; attempt <= m.config.Retries; attempt++ {
		check.StatusCode, check.ResponseTime, check.Error = m.request(ctx, site.URL)
		check.Available = check.Error == "" && check.StatusCode >= 200 && check.StatusCode < 500
		if check.Available || ctx.Err() != nil {
			break
		}
	}
	if err := m.store.RecordCheck(ctx, check); err != nil {
		m.logger.Error("save check result", "site", site.URL, "error", err)
		return
	}
	if previous.ID != 0 && previous.Available != check.Available {
		state := "down"
		if check.Available {
			state = "recovered"
		}
		m.logger.Warn("site availability changed", "site", site.URL, "state", state,
			"status_code", check.StatusCode, "error", check.Error)
	}
	m.logger.Info("site checked", "site", site.URL, "status_code", check.StatusCode,
		"available", check.Available, "response_time", check.ResponseTime, "error", check.Error)
}

func (m *Monitor) request(ctx context.Context, target string) (int, time.Duration, string) {
	requestCtx, cancel := context.WithTimeout(ctx, m.config.Timeout)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, target, nil)
	if err != nil {
		return 0, 0, err.Error()
	}
	started := time.Now()
	response, err := m.client.Do(request)
	elapsed := time.Since(started)
	if err != nil {
		return 0, elapsed, err.Error()
	}
	_, readErr := io.Copy(io.Discard, response.Body)
	closeErr := response.Body.Close()
	if readErr != nil {
		return response.StatusCode, elapsed, readErr.Error()
	}
	if closeErr != nil && !errors.Is(closeErr, context.Canceled) {
		return response.StatusCode, elapsed, closeErr.Error()
	}
	return response.StatusCode, elapsed, ""
}
