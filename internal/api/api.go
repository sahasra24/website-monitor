package api

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"website-monitor/internal/store"
)

type API struct {
	store  *store.Store
	logger *slog.Logger
}

func New(database *store.Store, logger *slog.Logger) http.Handler {
	a := &API{store: database, logger: logger}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", a.health)
	mux.HandleFunc("GET /api/sites", a.listSites)
	mux.HandleFunc("POST /api/sites", a.addSite)
	mux.HandleFunc("GET /api/summary", a.summaries)
	mux.HandleFunc("GET /api/sites/{id}/history", a.history)
	return mux
}

func (a *API) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *API) listSites(w http.ResponseWriter, r *http.Request) {
	sites, err := a.store.Sites(r.Context())
	if err != nil {
		a.serverError(w, "list sites", err)
		return
	}
	writeJSON(w, http.StatusOK, sites)
}

func (a *API) addSite(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var input struct {
		Name string `json:"name"`
		URL  string `json:"url"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON request body")
		return
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "request body must contain one JSON object")
		return
	}

	site, err := a.store.AddSite(r.Context(), input.Name, input.URL)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			writeError(w, http.StatusConflict, "a site with that URL already exists")
			return
		}
		if strings.HasPrefix(err.Error(), "url must be") {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		a.serverError(w, "add site", err)
		return
	}
	writeJSON(w, http.StatusCreated, site)
}

func (a *API) summaries(w http.ResponseWriter, r *http.Request) {
	summaries, err := a.store.Summaries(r.Context())
	if err != nil {
		a.serverError(w, "get summaries", err)
		return
	}
	writeJSON(w, http.StatusOK, summaries)
}

func (a *API) history(w http.ResponseWriter, r *http.Request) {
	siteID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || siteID < 1 {
		writeError(w, http.StatusBadRequest, "site id must be a positive integer")
		return
	}
	limit := 100
	if value := r.URL.Query().Get("limit"); value != "" {
		limit, err = strconv.Atoi(value)
		if err != nil || limit < 1 || limit > 1000 {
			writeError(w, http.StatusBadRequest, "limit must be between 1 and 1000")
			return
		}
	}
	checks, err := a.store.History(r.Context(), siteID, limit)
	if err != nil {
		a.serverError(w, "get check history", err)
		return
	}
	writeJSON(w, http.StatusOK, checks)
}

func (a *API) serverError(w http.ResponseWriter, operation string, err error) {
	a.logger.Error(operation, "error", err)
	writeError(w, http.StatusInternalServerError, "internal server error")
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		slog.Error("write JSON response", "error", err)
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
