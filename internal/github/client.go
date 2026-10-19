// Package github updates one BuildLens summary comment per allowed pull request.
package github

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	gh "github.com/google/go-github/v89/github"
)

const Marker = "<!-- buildlens:summary:v1 -->"

// HTTPError deliberately omits remote response bodies, which may contain secrets.
type HTTPError struct {
	Status  int
	RetryAt time.Time
}

func (e *HTTPError) Error() string { return fmt.Sprintf("GitHub HTTP %d", e.Status) }

type Client struct {
	api     *gh.Client
	allowed map[string]bool
}

func NewClient(token string, repositories []string) (*Client, error) {
	httpClient := &http.Client{Timeout: 10 * time.Second, Transport: limitedTransport{}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	allowed := map[string]bool{}
	for _, repo := range repositories {
		allowed[repo] = true
	}
	api, err := gh.NewClient(gh.WithHTTPClient(httpClient), gh.WithAuthToken(token))
	if err != nil {
		return nil, errors.New("invalid GitHub client configuration")
	}
	return &Client{api: api, allowed: allowed}, nil
}

// Sync searches all bounded pages before creating, so retrying after an uncertain
// POST response finds the original comment. A copied marker from another author
// is never edited. Closed PRs are acknowledged without a write. The summary names
// its tested commit: Jenkins merge-build SHAs need not equal a PR's head SHA.
func (c *Client) Sync(ctx context.Context, repository string, number int, sha, body string) (*int64, error) {
	if !c.allowed[repository] {
		return nil, errors.New("GitHub repository is outside the comment allowlist")
	}
	owner, repo, ok := strings.Cut(repository, "/")
	if !ok || owner == "" || repo == "" || strings.Contains(repo, "/") || number < 1 || !strings.HasPrefix(body, Marker+"\n") || len(body) > 60_000 {
		return nil, errors.New("invalid GitHub comment request")
	}
	pr, resp, err := c.api.PullRequests.Get(ctx, owner, repo, number)
	if err != nil {
		return nil, remoteError(resp, err)
	}
	if pr == nil || pr.Head == nil {
		return nil, errors.New("GitHub PR response lacks head")
	}
	if pr.GetState() != "open" {
		return nil, nil
	}
	actor, resp, err := c.api.Users.Get(ctx, "")
	if err != nil {
		return nil, remoteError(resp, err)
	}
	if actor == nil || actor.GetLogin() == "" {
		return nil, errors.New("GitHub response lacks authenticated actor")
	}
	var found *gh.IssueComment
	page := 1
	for scanned := 0; ; scanned++ {
		if scanned >= 20 {
			return nil, errors.New("GitHub comment search exceeded 20 pages; refusing duplicate creation")
		}
		comments, response, listErr := c.api.Issues.ListComments(ctx, owner, repo, number, &gh.IssueListCommentsOptions{ListOptions: gh.ListOptions{Page: page, PerPage: 100}})
		if listErr != nil {
			return nil, remoteError(response, listErr)
		}
		for _, comment := range comments {
			if comment != nil && comment.User != nil && comment.GetID() > 0 && strings.EqualFold(comment.User.GetLogin(), actor.GetLogin()) && strings.HasPrefix(comment.GetBody(), Marker+"\n") {
				if found == nil || comment.GetID() < found.GetID() {
					found = comment
				}
			}
		}
		if response == nil || response.NextPage == 0 {
			break
		}
		if response.NextPage <= page {
			return nil, errors.New("GitHub comment pagination did not advance")
		}
		page = response.NextPage
	}
	request := &gh.IssueComment{Body: gh.Ptr(body)}
	var comment *gh.IssueComment
	if found != nil {
		if found.GetBody() == body {
			id := found.GetID()
			return &id, nil
		}
		comment, resp, err = c.api.Issues.EditComment(ctx, owner, repo, found.GetID(), request)
	} else {
		comment, resp, err = c.api.Issues.CreateComment(ctx, owner, repo, number, request)
	}
	if err != nil {
		return nil, remoteError(resp, err)
	}
	if comment == nil || comment.GetID() < 1 {
		return nil, errors.New("GitHub comment response lacks id")
	}
	id := comment.GetID()
	return &id, nil
}

type limitedTransport struct{}
type limitedBody struct {
	io.Reader
	io.Closer
}

func (limitedTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	resp, err := http.DefaultTransport.RoundTrip(r)
	if err != nil {
		return nil, err
	}
	resp.Body = limitedBody{Reader: io.LimitReader(resp.Body, 2<<20), Closer: resp.Body}
	return resp, nil
}

func remoteError(resp *gh.Response, err error) error {
	if resp == nil {
		if errors.Is(err, context.Canceled) {
			return context.Canceled
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return context.DeadlineExceeded
		}
		return errors.New("GitHub transport or response decode failed")
	}
	e := &HTTPError{Status: resp.StatusCode}
	if seconds, parseErr := strconv.Atoi(resp.Header.Get("Retry-After")); parseErr == nil && seconds > 0 {
		e.RetryAt = time.Now().Add(time.Duration(min(seconds, 86400)) * time.Second)
	} else if t, parseErr := http.ParseTime(resp.Header.Get("Retry-After")); parseErr == nil {
		e.RetryAt = t
	}
	if (e.Status == http.StatusForbidden || e.Status == http.StatusTooManyRequests) && resp.Header.Get("X-RateLimit-Remaining") == "0" {
		if unix, parseErr := strconv.ParseInt(resp.Header.Get("X-RateLimit-Reset"), 10, 64); parseErr == nil {
			t := time.Unix(unix, 0)
			if t.After(e.RetryAt) {
				e.RetryAt = t
			}
		}
	}
	return e
}
