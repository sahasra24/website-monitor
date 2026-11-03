package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

type Site struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	URL       string    `json:"url"`
	CreatedAt time.Time `json:"created_at"`
}

type Check struct {
	ID           int64         `json:"id"`
	SiteID       int64         `json:"site_id"`
	CheckedAt    time.Time     `json:"checked_at"`
	StatusCode   int           `json:"status_code"`
	ResponseTime time.Duration `json:"response_time_ns"`
	Available    bool          `json:"available"`
	Error        string        `json:"error,omitempty"`
}

type Summary struct {
	Site
	TotalChecks   int64     `json:"total_checks"`
	Successful    int64     `json:"successful_checks"`
	UptimePercent float64   `json:"uptime_percent"`
	LastCheckedAt time.Time `json:"last_checked_at,omitempty"`
	LastStatus    int       `json:"last_status_code"`
	IsUp          bool      `json:"is_up"`
}

func Open(path string) (*Store, error) {
	dsn := path
	if path != ":memory:" && !strings.HasPrefix(path, "file:") {
		dsn = "file:" + url.PathEscape(path)
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`
		PRAGMA journal_mode = WAL;
		PRAGMA foreign_keys = ON;
		CREATE TABLE IF NOT EXISTS sites (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			url TEXT NOT NULL UNIQUE,
			created_at DATETIME NOT NULL
		);
		CREATE TABLE IF NOT EXISTS checks (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			site_id INTEGER NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
			checked_at DATETIME NOT NULL,
			status_code INTEGER NOT NULL,
			response_time_ns INTEGER NOT NULL,
			available INTEGER NOT NULL,
			error TEXT NOT NULL DEFAULT ''
		);
		CREATE INDEX IF NOT EXISTS checks_site_checked_at ON checks(site_id, checked_at DESC);
	`); err != nil {
		db.Close()
		return nil, fmt.Errorf("initialize sqlite schema: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) AddSite(ctx context.Context, name, rawURL string) (Site, error) {
	name = strings.TrimSpace(name)
	parsed, err := url.ParseRequestURI(strings.TrimSpace(rawURL))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return Site{}, errors.New("url must be an absolute http or https URL")
	}
	if name == "" {
		name = parsed.Host
	}
	createdAt := time.Now().UTC()
	result, err := s.db.ExecContext(ctx,
		"INSERT INTO sites(name, url, created_at) VALUES(?, ?, ?)", name, parsed.String(), createdAt)
	if err != nil {
		return Site{}, fmt.Errorf("insert site: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return Site{}, fmt.Errorf("get inserted site id: %w", err)
	}
	return Site{ID: id, Name: name, URL: parsed.String(), CreatedAt: createdAt}, nil
}

func (s *Store) Sites(ctx context.Context) ([]Site, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id, name, url, created_at FROM sites ORDER BY id")
	if err != nil {
		return nil, fmt.Errorf("query sites: %w", err)
	}
	defer rows.Close()
	sites := make([]Site, 0)
	for rows.Next() {
		var site Site
		if err := rows.Scan(&site.ID, &site.Name, &site.URL, &site.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan site: %w", err)
		}
		sites = append(sites, site)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sites: %w", err)
	}
	return sites, nil
}

func (s *Store) RecordCheck(ctx context.Context, check Check) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO checks(site_id, checked_at, status_code, response_time_ns, available, error)
		VALUES(?, ?, ?, ?, ?, ?)`,
		check.SiteID, check.CheckedAt, check.StatusCode, check.ResponseTime.Nanoseconds(), check.Available, check.Error)
	if err != nil {
		return fmt.Errorf("record check: %w", err)
	}
	return nil
}

func (s *Store) LatestCheck(ctx context.Context, siteID int64) (Check, error) {
	var check Check
	var available int
	err := s.db.QueryRowContext(ctx, `
		SELECT id, site_id, checked_at, status_code, response_time_ns, available, error
		FROM checks WHERE site_id = ? ORDER BY checked_at DESC, id DESC LIMIT 1`, siteID).
		Scan(&check.ID, &check.SiteID, &check.CheckedAt, &check.StatusCode, &check.ResponseTime, &available, &check.Error)
	check.Available = available != 0
	if errors.Is(err, sql.ErrNoRows) {
		return Check{}, nil
	}
	if err != nil {
		return Check{}, fmt.Errorf("query latest check: %w", err)
	}
	return check, nil
}

func (s *Store) Summaries(ctx context.Context) ([]Summary, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT s.id, s.name, s.url, s.created_at,
			COUNT(c.id),
			COALESCE(SUM(CASE WHEN c.available = 1 THEN 1 ELSE 0 END), 0),
			(SELECT checked_at FROM checks WHERE site_id = s.id ORDER BY checked_at DESC, id DESC LIMIT 1),
			COALESCE((SELECT status_code FROM checks WHERE site_id = s.id ORDER BY checked_at DESC, id DESC LIMIT 1), 0),
			COALESCE((SELECT available FROM checks WHERE site_id = s.id ORDER BY checked_at DESC, id DESC LIMIT 1), 0)
		FROM sites s LEFT JOIN checks c ON c.site_id = s.id GROUP BY s.id ORDER BY s.id`)
	if err != nil {
		return nil, fmt.Errorf("query summaries: %w", err)
	}
	defer rows.Close()
	summaries := make([]Summary, 0)
	for rows.Next() {
		var summary Summary
		var lastChecked sql.NullTime
		var isUp int
		if err := rows.Scan(&summary.ID, &summary.Name, &summary.URL, &summary.CreatedAt,
			&summary.TotalChecks, &summary.Successful, &lastChecked, &summary.LastStatus, &isUp); err != nil {
			return nil, fmt.Errorf("scan summary: %w", err)
		}
		if lastChecked.Valid {
			summary.LastCheckedAt = lastChecked.Time
		}
		if summary.TotalChecks > 0 {
			summary.UptimePercent = float64(summary.Successful) / float64(summary.TotalChecks) * 100
		}
		summary.IsUp = isUp != 0
		summaries = append(summaries, summary)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate summaries: %w", err)
	}
	return summaries, nil
}

func (s *Store) History(ctx context.Context, siteID int64, limit int) ([]Check, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, site_id, checked_at, status_code, response_time_ns, available, error
		FROM checks WHERE site_id = ? ORDER BY checked_at DESC, id DESC LIMIT ?`, siteID, limit)
	if err != nil {
		return nil, fmt.Errorf("query history: %w", err)
	}
	defer rows.Close()
	checks := make([]Check, 0)
	for rows.Next() {
		var check Check
		var available int
		if err := rows.Scan(&check.ID, &check.SiteID, &check.CheckedAt, &check.StatusCode,
			&check.ResponseTime, &available, &check.Error); err != nil {
			return nil, fmt.Errorf("scan check: %w", err)
		}
		check.Available = available != 0
		checks = append(checks, check)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate history: %w", err)
	}
	return checks, nil
}
