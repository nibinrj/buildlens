package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nibinrj/buildlens/internal/api"
	"github.com/nibinrj/buildlens/internal/ingest"
	"github.com/nibinrj/buildlens/internal/quarantine"
	"github.com/nibinrj/buildlens/internal/upload"
)

// This is a local API simulation backed by real Postgres, not a published GitHub PR.
func TestHTTPIngestAndStickyComment(t *testing.T) {
	pool, svc := commentDB(t)
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	quarantines := quarantine.NewService(pool, quarantine.Defaults())
	var mu sync.Mutex
	created, edited := 0, 0
	var saved string
	client, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch r.URL.Path {
		case "/user":
			fmt.Fprint(w, `{"login":"bot"}`)
		case "/repos/owner/lab/pulls/7":
			fmt.Fprint(w, `{"state":"open","head":{"sha":"different-head-from-jenkins-merge"}}`)
		default:
			if r.Method == http.MethodGet {
				if saved == "" {
					fmt.Fprint(w, `[]`)
				} else {
					_ = json.NewEncoder(w).Encode([]map[string]any{{"id": 91, "body": saved, "user": map[string]string{"login": "bot"}}})
				}
				return
			}
			var request map[string]string
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Error(err)
			}
			saved = request["body"]
			switch r.Method {
			case http.MethodPost:
				created++
				w.WriteHeader(http.StatusCreated)
			case http.MethodPatch:
				edited++
			default:
				t.Errorf("unexpected method%s", r.Method)
			}
			fmt.Fprint(w, `{"id":91}`)
		}
	})
	worker := NewWorker(DatabaseStore{pool}, DatabaseSource{pool}, client, logger)
	server := httptest.NewServer(api.NewRouter(api.Deps{DB: pool, Ingester: svc, Builds: svc, Quarantine: quarantines, IngestKey: "test-only-key", MaxUploadBytes: 1 << 20, Logger: logger, NotifyBuild: worker.Notify}))
	defer server.Close()
	uploadBuild := func(number int32, pr bool, slow int64) ingest.Result {
		t.Helper()
		u := prUpload(number)
		u.Meta.StartedAt = u.Meta.StartedAt.Add(2 * time.Hour)
		if !pr {
			u.Meta.PRNumber = nil
			u.Meta.Branch = "main"
			u.Meta.Job = "lab/main"
		}
		u.Meta.Stages = []upload.Stage{{Name: "Test", StartedAt: u.Meta.StartedAt, DurationMs: slow, Result: "SUCCESS"}}
		var buf bytes.Buffer
		writer := multipart.NewWriter(&buf)
		part, err := writer.CreateFormField(upload.PartMetadata)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.NewEncoder(part).Encode(u.Meta); err != nil {
			t.Fatal(err)
		}
		report, err := writer.CreateFormFile(upload.PartReport, "api/target/surefire-reports/TEST-lab.SlowTest.xml")
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(report, `<testsuite><testcase classname="lab.SlowTest" name="work" time="%.3f"/></testsuite>`, float64(slow)/1000)
		if pr {
			h := make(textproto.MIMEHeader)
			h.Set("Content-Disposition", `form-data; name="report"; filename="core/target/surefire-reports-quarantine/TEST-lab.Test.xml"`)
			h.Set(upload.HeaderTestStage, "QUARANTINE")
			p, err := writer.CreatePart(h)
			if err != nil {
				t.Fatal(err)
			}
			fmt.Fprint(p, `<testsuite><testcase classname="lab.Test" name="x" time="0.001"/></testsuite>`)
		} else {
			p, err := writer.CreateFormFile(upload.PartReport, "core/target/surefire-reports/TEST-lab.Test.xml")
			if err != nil {
				t.Fatal(err)
			}
			fmt.Fprint(p, `<testsuite><testcase classname="lab.Test" name="x" time="0.001"/></testsuite>`)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+"/api/v1/builds", &buf)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer test-only-key")
		req.Header.Set("Content-Type", writer.FormDataContentType())
		response, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close() //nolint:errcheck // read-only test response
		var result ingest.Result
		if response.StatusCode != http.StatusCreated {
			data, _ := io.ReadAll(response.Body)
			t.Fatalf("%d %s", response.StatusCode, data)
		}
		if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	for n := int32(1); n <= 13; n++ {
		uploadBuild(n, false, 100)
	}
	if _, err := quarantines.CreateManual(ctx, quarantine.Request{Repo: "owner/lab", ClassName: "lab.Test", MethodName: "x", Reason: "synthetic API test quarantine"}); err != nil {
		t.Fatal(err)
	}
	for _, n := range []int32{30, 31} {
		result := uploadBuild(n, true, 1000)
		if did, err := worker.ProcessOne(ctx); err != nil || !did {
			t.Fatalf("%v %v", did, err)
		}
		mu.Lock()
		body := saved
		mu.Unlock()
		for _, section := range []string{"### Quarantined tests", "lab.Test#x", "### Regressions", "REGRESSION", "### Cost", "equivalent cost at reference price"} {
			if !strings.Contains(body, section) {
				t.Fatalf("missing %s in %s", section, body)
			}
		}
		t.Logf("local simulated GitHub API after build %d: comment id 91; BuildLens id %d", n, result.BuildID)
	}
	mu.Lock()
	defer mu.Unlock()
	if created != 1 || edited != 1 {
		t.Fatalf("create=%d edit=%d", created, edited)
	}
	t.Log("local simulated GitHub API: POST 201 followed by PATCH 200; same comment id; synthetic durations")
	t.Logf("COMMENT_PREVIEW_BEGIN\n%s\nCOMMENT_PREVIEW_END", saved)
}
