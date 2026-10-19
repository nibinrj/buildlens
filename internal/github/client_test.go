package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	gh "github.com/google/go-github/v89/github"
)

func testClient(t *testing.T, h http.HandlerFunc) (*Client, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(h)
	t.Cleanup(server.Close)
	c, clientErr := NewClient("test-only-token", []string{"owner/lab"})
	if clientErr != nil {
		t.Fatal(clientErr)
	}
	var err error
	c.api, err = gh.NewClient(gh.WithTransport(limitedTransport{}), gh.WithAuthToken("test-only-token"), gh.WithURLs(gh.Ptr(server.URL+"/"), nil))
	if err != nil {
		t.Fatal(err)
	}
	return c, server
}

func TestStickyComment(t *testing.T) {
	var mu sync.Mutex
	created, edited := 0, 0
	var saved string
	c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer test-only-token" {
			t.Error("missing auth")
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/user":
			fmt.Fprint(w, `{"login":"bot"}`)
		case r.URL.Path == "/repos/owner/lab/pulls/7":
			fmt.Fprint(w, `{"state":"open","head":{"sha":"abc"}}`)
		case r.Method == http.MethodGet && r.URL.Path == "/repos/owner/lab/issues/7/comments":
			// The first page contains an attacker copying our marker.
			if r.URL.Query().Get("page") == "1" {
				w.Header().Set("Link", fmt.Sprintf(`<http://%s%s?page=2>; rel="next"`, r.Host, r.URL.Path))
				_ = json.NewEncoder(w).Encode([]gh.IssueComment{{ID: gh.Ptr(int64(1)), Body: gh.Ptr(Marker + "\nspoof"), User: &gh.User{Login: gh.Ptr("someone-else")}}})
			} else if saved == "" {
				fmt.Fprint(w, `[]`)
			} else {
				_ = json.NewEncoder(w).Encode([]gh.IssueComment{{ID: gh.Ptr(int64(91)), Body: &saved, User: &gh.User{Login: gh.Ptr("bot")}}})
			}
		case r.Method == http.MethodPost && r.URL.Path == "/repos/owner/lab/issues/7/comments":
			created++
			var b gh.IssueComment
			if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
				t.Error(err)
			}
			saved = b.GetBody()
			w.WriteHeader(http.StatusCreated)
			fmt.Fprint(w, `{"id":91}`)
		case r.Method == http.MethodPatch && r.URL.Path == "/repos/owner/lab/issues/comments/91":
			edited++
			var b gh.IssueComment
			if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
				t.Error(err)
			}
			saved = b.GetBody()
			fmt.Fprint(w, `{"id":91}`)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL)
			w.WriteHeader(http.StatusNotFound)
		}
	})
	for _, body := range []string{Marker + "\nfirst", Marker + "\nsecond", Marker + "\nsecond"} {
		id, err := c.Sync(context.Background(), "owner/lab", 7, "abc", body)
		if err != nil || id == nil || *id != 91 {
			t.Fatalf("%v %v", id, err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if created != 1 || edited != 1 {
		t.Fatalf("creates=%d edits=%d", created, edited)
	}
}

func TestGitHubFailuresAndGuards(t *testing.T) {
	for _, tc := range []struct {
		name       string
		status     int
		state, sha string
		wantSkip   bool
	}{
		{"forbidden", 403, "open", "abc", false}, {"server error", 500, "open", "abc", false}, {"rate limited", 429, "open", "abc", false},
		{"closed", 200, "closed", "abc", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			writes := 0
			c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					writes++
				}
				if r.URL.Path == "/repos/owner/lab/pulls/7" {
					fmt.Fprintf(w, `{"state":%q,"head":{"sha":%q}}`, tc.state, tc.sha)
					return
				}
				w.Header().Set("Retry-After", "120")
				w.WriteHeader(tc.status)
				fmt.Fprint(w, `{"message":"private-token-must-not-be-logged"}`)
			})
			id, err := c.Sync(context.Background(), "owner/lab", 7, "abc", Marker+"\nbody")
			if tc.wantSkip {
				if err != nil || id != nil {
					t.Fatalf("%v %v", id, err)
				}
			} else {
				var remote *HTTPError
				if !errors.As(err, &remote) || remote.Status != tc.status || remote.RetryAt.IsZero() || strings.Contains(err.Error(), "private-token") {
					t.Fatalf("%v", err)
				}
			}
			if writes != 0 {
				t.Fatal("guard permitted write")
			}
		})
	}
	t.Run("allowlist and body guards", func(t *testing.T) {
		c, _ := testClient(t, func(http.ResponseWriter, *http.Request) { t.Error("request should be rejected locally") })
		for _, v := range []struct {
			repo, body string
			pr         int
		}{{"outsider/repo", Marker + "\nx", 7}, {"owner/lab", "bad", 7}, {"owner/lab", Marker + "\nx", 0}, {"owner/lab", Marker + "\n" + strings.Repeat("x", 60001), 7}} {
			if _, err := c.Sync(context.Background(), v.repo, v.pr, "abc", v.body); err == nil {
				t.Fatal("accepted invalid request")
			}
		}
	})
	t.Run("cancellation", func(t *testing.T) {
		c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() })
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
		defer cancel()
		if _, err := c.Sync(ctx, "owner/lab", 7, "abc", Marker+"\nx"); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("%v", err)
		}
	})
}

func TestCommentWriteFailures(t *testing.T) {
	for _, status := range []int{403, 500, 201} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/repos/owner/lab/pulls/7":
					fmt.Fprint(w, `{"state":"open","head":{"sha":"abc"}}`)
				case "/user":
					fmt.Fprint(w, `{"login":"bot"}`)
				default:
					if r.Method == http.MethodGet {
						fmt.Fprint(w, `[]`)
					} else {
						w.WriteHeader(status)
						fmt.Fprint(w, `{"message":"secret"}`)
					}
				}
			})
			if _, err := c.Sync(context.Background(), "owner/lab", 7, "abc", Marker+"\nx"); err == nil || strings.Contains(err.Error(), "secret") {
				t.Fatalf("%v", err)
			}
		})
	}
}

func TestUncertainPostIsRecoveredWithoutDuplicate(t *testing.T) {
	var mu sync.Mutex
	posts, patches := 0, 0
	body := ""
	c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch r.URL.Path {
		case "/repos/owner/lab/pulls/7":
			fmt.Fprint(w, `{"state":"open","head":{"sha":"merge"}}`)
		case "/user":
			fmt.Fprint(w, `{"login":"bot"}`)
		default:
			switch r.Method {
			case http.MethodGet:
				if body == "" {
					fmt.Fprint(w, `[]`)
				} else {
					_ = json.NewEncoder(w).Encode([]gh.IssueComment{{ID: gh.Ptr(int64(91)), Body: &body, User: &gh.User{Login: gh.Ptr("bot")}}})
				}
			case http.MethodPost:
				posts++
				body = Marker + "\noriginal"
				w.WriteHeader(http.StatusServiceUnavailable)
				fmt.Fprint(w, `{"message":"result uncertain"}`)
			case http.MethodPatch:
				patches++
				fmt.Fprint(w, `{"id":91}`)
			}
		}
	})
	if _, err := c.Sync(context.Background(), "owner/lab", 7, "merge", Marker+"\noriginal"); err == nil {
		t.Fatal("503 not reported")
	}
	if id, err := c.Sync(context.Background(), "owner/lab", 7, "merge", Marker+"\nupdated"); err != nil || id == nil || *id != 91 {
		t.Fatalf("%v %v", id, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if posts != 1 || patches != 1 {
		t.Fatalf("posts=%d patches=%d", posts, patches)
	}
}

func TestBoundedCommentSearch(t *testing.T) {
	for _, large := range []bool{false, true} {
		t.Run(fmt.Sprint(large), func(t *testing.T) {
			c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/repos/owner/lab/pulls/7":
					fmt.Fprint(w, `{"state":"open","head":{"sha":"abc"}}`)
				case "/user":
					fmt.Fprint(w, `{"login":"bot"}`)
				default:
					if r.Method != http.MethodGet {
						t.Error("unbounded search created a comment")
					}
					if large {
						fmt.Fprintf(w, `[{"body":"%s"}]`, strings.Repeat("x", (2<<20)+1))
						return
					}
					var page int
					_, _ = fmt.Sscan(r.URL.Query().Get("page"), &page)
					w.Header().Set("Link", fmt.Sprintf(`<http://%s%s?page=%d>; rel="next"`, r.Host, r.URL.Path, page+1))
					fmt.Fprint(w, `[]`)
				}
			})
			if _, err := c.Sync(context.Background(), "owner/lab", 7, "abc", Marker+"\nx"); err == nil {
				t.Fatal("unbounded response accepted")
			}
		})
	}
}
