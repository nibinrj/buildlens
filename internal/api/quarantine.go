package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/nibinrj/buildlens/internal/quarantine"
)

// QuarantineService is consumed by HTTP handlers and replaced by a fake in handler tests.
type QuarantineService interface {
	List(context.Context, string) ([]quarantine.Entry, error)
	CreateManual(context.Context, quarantine.Request) (int64, error)
	ReleaseManual(context.Context, int64, string) error
}

func validRepo(repo string) bool {
	owner, name, ok := strings.Cut(repo, "/")
	return ok && owner != "" && name != "" && !strings.Contains(name, "/") && len(repo) <= 200
}
func handleListQuarantine(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		repo := r.URL.Query().Get("repo")
		if !validRepo(repo) {
			writeError(w, http.StatusBadRequest, "repo must be owner/name")
			return
		}
		list, err := d.Quarantine.List(r.Context(), repo)
		if err != nil {
			quarantineError(w, r, d, err)
			return
		}
		if list == nil {
			list = []quarantine.Entry{}
		}
		writeJSON(w, http.StatusOK, list)
	}
}
func handleCreateQuarantine(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var request quarantine.Request
		if !decodeQuarantineBody(w, r, &request) {
			return
		}
		if !validRepo(request.Repo) || strings.TrimSpace(request.ClassName) == "" || strings.TrimSpace(request.MethodName) == "" ||
			strings.TrimSpace(request.Reason) == "" || len(request.Reason) > 2000 {
			writeError(w, http.StatusBadRequest, "repo, class_name, method_name and reason (up to 2000 characters) are required")
			return
		}
		id, err := d.Quarantine.CreateManual(r.Context(), request)
		if err != nil {
			quarantineError(w, r, d, err)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]int64{"id": id})
	}
}
func handleReleaseQuarantine(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil || id < 1 {
			writeError(w, http.StatusBadRequest, "quarantine id must be positive")
			return
		}
		var request struct {
			Reason string `json:"reason"`
		}
		if !decodeQuarantineBody(w, r, &request) {
			return
		}
		if strings.TrimSpace(request.Reason) == "" || len(request.Reason) > 2000 {
			writeError(w, http.StatusBadRequest, "reason is required (up to 2000 characters)")
			return
		}
		if err := d.Quarantine.ReleaseManual(r.Context(), id, request.Reason); err != nil {
			quarantineError(w, r, d, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"released": true})
	}
}
func decodeQuarantineBody(w http.ResponseWriter, r *http.Request, out any) bool {
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON request")
		return false
	}
	var extra any
	if err := d.Decode(&extra); !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "expected one JSON request")
		return false
	}
	return true
}
func quarantineError(w http.ResponseWriter, r *http.Request, d Deps, err error) {
	switch {
	case errors.Is(err, quarantine.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, quarantine.ErrConflict), errors.Is(err, quarantine.ErrCap):
		writeError(w, http.StatusConflict, err.Error())
	default:
		d.Logger.ErrorContext(r.Context(), "quarantine request failed", "err", err)
		writeError(w, http.StatusInternalServerError, "quarantine operation failed")
	}
}
