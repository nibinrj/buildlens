package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"

	"github.com/nibinrj/buildlens/internal/ingest"
	"github.com/nibinrj/buildlens/internal/surefire"
	"github.com/nibinrj/buildlens/internal/upload"
)

// Ingester stores an upload. *ingest.Service satisfies it; handler tests use a fake.
type Ingester interface {
	Ingest(ctx context.Context, u ingest.Upload) (ingest.Result, error)
}

// BuildReader reads one build. *ingest.Service satisfies it.
type BuildReader interface {
	GetBuild(ctx context.Context, id int64) (ingest.BuildView, error)
}

// requireKey rejects requests without "Authorization: Bearer <key>". The comparison takes the same time whatever
// the input, so the key cannot be guessed byte by byte from response times.
func requireKey(key string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
			if !ok || key == "" || subtle.ConstantTimeCompare([]byte(got), []byte(key)) != 1 {
				w.Header().Set("WWW-Authenticate", `Bearer realm="buildlens"`)
				writeError(w, http.StatusUnauthorized, "missing or invalid API key")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// handleCreateBuild is POST /api/v1/builds: a multipart upload of metadata, reports and the log tail.
// Parts are read as they stream in, so a large upload never sits in memory. Nothing is stored unless
// every part is valid.
func handleCreateBuild(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, d.MaxUploadBytes)

		mr, err := r.MultipartReader()
		if err != nil {
			writeError(w, http.StatusUnsupportedMediaType, "expected a multipart/form-data body")
			return
		}

		var (
			meta    *upload.Metadata
			tests   []surefire.Result
			reports int
		)
		for {
			part, err := mr.NextPart()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				writeReadError(w, err, "read multipart body")
				return
			}
			status, msg := readPart(part, &meta, &tests, &reports)
			_ = part.Close()
			if status != 0 {
				writeError(w, status, msg)
				return
			}
		}

		if meta == nil {
			writeError(w, http.StatusBadRequest, "missing the metadata part")
			return
		}
		if err := meta.Validate(); err != nil {
			writeError(w, http.StatusBadRequest, "invalid metadata: "+err.Error())
			return
		}

		res, err := d.Ingester.Ingest(r.Context(), ingest.Upload{Meta: *meta, Tests: tests})
		if err != nil {
			d.Logger.ErrorContext(r.Context(), "ingest failed", "job", meta.Job, "build_number", meta.BuildNumber, "err", err)
			writeError(w, http.StatusInternalServerError, "could not store the build")
			return
		}

		d.Logger.InfoContext(r.Context(), "build ingested", "build_id", res.BuildID, "created", res.Created,
			"job", meta.Job, "build_number", meta.BuildNumber, "reports", reports, "tests", res.Tests, "stages", res.Stages)
		status := http.StatusOK
		if res.Created {
			status = http.StatusCreated
		}
		writeJSON(w, status, res)
	}
}

// readPart handles one multipart part. It returns a non-zero HTTP status and a message when the part is invalid.
func readPart(part *multipart.Part, meta **upload.Metadata, tests *[]surefire.Result, reports *int) (int, string) {
	switch part.FormName() {
	case upload.PartMetadata:
		if *meta != nil {
			return http.StatusBadRequest, "more than one metadata part"
		}
		var m upload.Metadata
		dec := json.NewDecoder(io.LimitReader(part, upload.MaxMetadataBytes))
		if err := dec.Decode(&m); err != nil {
			if isTooLarge(err) {
				return http.StatusRequestEntityTooLarge, "upload exceeds the size limit"
			}
			return http.StatusBadRequest, "metadata is not valid JSON: " + err.Error()
		}
		*meta = &m

	case upload.PartReport:
		name := rawFileName(part)
		results, err := surefire.Parse(part, name)
		if err != nil {
			if isTooLarge(err) {
				return http.StatusRequestEntityTooLarge, "upload exceeds the size limit"
			}
			return http.StatusBadRequest, "invalid report: " + err.Error()
		}
		*tests = append(*tests, results...)
		*reports++

	case upload.PartLog:
		// P3's infrastructure rules read the log tail; P2 only checks its size.
		n, err := io.Copy(io.Discard, io.LimitReader(part, upload.MaxLogBytes+1))
		if err != nil {
			if isTooLarge(err) {
				return http.StatusRequestEntityTooLarge, "upload exceeds the size limit"
			}
			return http.StatusBadRequest, "read log part: " + err.Error()
		}
		if n > upload.MaxLogBytes {
			return http.StatusRequestEntityTooLarge, fmt.Sprintf("log part is larger than %d bytes", upload.MaxLogBytes)
		}

	default:
		return http.StatusBadRequest, fmt.Sprintf("unknown part %q", part.FormName())
	}
	return 0, ""
}

// rawFileName returns the filename exactly as the client sent it. multipart.Part.FileName applies
// filepath.Base, which would drop "core/target/..." and with it the Maven module. The value is only used as a
// label (to find the module), never as a path on disk, so keeping directories is safe.
func rawFileName(p *multipart.Part) string {
	_, params, err := mime.ParseMediaType(p.Header.Get("Content-Disposition"))
	if err != nil {
		return p.FileName()
	}
	return params["filename"]
}

// handleGetBuild is GET /api/v1/builds/{id}, for debugging.
func handleGetBuild(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil || id <= 0 {
			writeError(w, http.StatusBadRequest, "build id must be a positive integer")
			return
		}
		b, err := d.Builds.GetBuild(r.Context(), id)
		if errors.Is(err, ingest.ErrNotFound) {
			writeError(w, http.StatusNotFound, "build not found")
			return
		}
		if err != nil {
			d.Logger.ErrorContext(r.Context(), "get build failed", "build_id", id, "err", err)
			writeError(w, http.StatusInternalServerError, "could not read the build")
			return
		}
		writeJSON(w, http.StatusOK, b)
	}
}

func isTooLarge(err error) bool {
	var maxErr *http.MaxBytesError
	return errors.As(err, &maxErr)
}

func writeReadError(w http.ResponseWriter, err error, what string) {
	if isTooLarge(err) {
		writeError(w, http.StatusRequestEntityTooLarge, "upload exceeds the size limit")
		return
	}
	writeError(w, http.StatusBadRequest, what+": "+err.Error())
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
