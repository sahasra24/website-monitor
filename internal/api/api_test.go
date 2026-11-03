package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"website-monitor/internal/store"
)

func TestAddSiteAndReadSummary(t *testing.T) {
	database, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	server := httptest.NewServer(New(database, slog.New(slog.NewTextHandler(io.Discard, nil))))
	defer server.Close()

	response, err := http.Post(server.URL+"/api/sites", "application/json",
		bytes.NewBufferString(`{"name":"Example","url":"https://example.com"}`))
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("POST status = %d, want %d", response.StatusCode, http.StatusCreated)
	}
	var site store.Site
	if err := json.NewDecoder(response.Body).Decode(&site); err != nil {
		response.Body.Close()
		t.Fatal(err)
	}
	response.Body.Close()

	if err := database.RecordCheck(context.Background(), store.Check{
		SiteID: site.ID, StatusCode: 200, Available: true,
	}); err != nil {
		t.Fatal(err)
	}
	response, err = http.Get(server.URL + "/api/summary")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET summary status = %d, want %d", response.StatusCode, http.StatusOK)
	}
	var summaries []store.Summary
	if err := json.NewDecoder(response.Body).Decode(&summaries); err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 1 || summaries[0].UptimePercent != 100 {
		t.Fatalf("unexpected summaries: %+v", summaries)
	}
}

func TestAddSiteRejectsMultipleJSONValues(t *testing.T) {
	database, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	handler := New(database, slog.New(slog.NewTextHandler(io.Discard, nil)))
	request := httptest.NewRequest(http.MethodPost, "/api/sites", bytes.NewBufferString(
		`{"url":"https://example.com"} {"url":"https://other.example"}`))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusBadRequest)
	}
}
