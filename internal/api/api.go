// Package api is a small client for the easymarkdown REST API (/api/v1).
// The contract is https://easymarkdown.io/docs/api/openapi.json.
package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Exit codes, one per error class, so scripts and agents can branch without parsing text.
const (
	ExitOK         = 0
	ExitUnexpected = 1
	ExitUsage      = 2
	ExitConflict   = 3
	ExitAuth       = 4
	ExitPayment    = 5
	ExitNotFound   = 6
	ExitRateLimit  = 7
	ExitValidation = 8
)

// Error is an API error: the HTTP status plus the server's machine-readable code.
type Error struct {
	Status  int
	Code    string
	Message string
	Body    []byte // the raw response body, for --json
}

func (e *Error) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("%s: %s", e.Code, e.Message)
	}
	return fmt.Sprintf("HTTP %d: %s", e.Status, e.Message)
}

// ExitCode maps an error to the CLI's exit code.
func ExitCode(err error) int {
	var e *Error
	if !errors.As(err, &e) {
		return ExitUnexpected
	}
	switch e.Code {
	case "conflict", "folder_not_empty":
		return ExitConflict
	case "unauthenticated", "missing_ability":
		return ExitAuth
	case "subscription_required", "subscription_ended":
		return ExitPayment
	case "not_found":
		return ExitNotFound
	case "rate_limited":
		return ExitRateLimit
	case "validation_failed", "content_too_large", "storage_limit", "row_limit":
		return ExitValidation
	}
	switch {
	case e.Status == 401 || e.Status == 403:
		return ExitAuth
	case e.Status == 402:
		return ExitPayment
	case e.Status == 404:
		return ExitNotFound
	case e.Status == 409:
		return ExitConflict
	case e.Status == 422:
		return ExitValidation
	case e.Status == 429:
		return ExitRateLimit
	}
	return ExitUnexpected
}

// Client talks to one easymarkdown API base URL with one key.
type Client struct {
	Base      string // e.g. https://easymarkdown.io/api/v1
	Token     string
	HTTP      *http.Client
	UserAgent string
	Sleep     func(time.Duration) // replaceable in tests

	remaining int // last X-RateLimit-Remaining seen; -1 = unknown
}

// New returns a client with sane defaults.
func New(base, token, version string) *Client {
	return &Client{
		Base:      strings.TrimRight(base, "/"),
		Token:     token,
		HTTP:      &http.Client{Timeout: 60 * time.Second},
		UserAgent: "emd/" + version,
		Sleep:     time.Sleep,
		remaining: -1,
	}
}

// Request is one HTTP call. Body is sent as-is with ContentType (JSON when empty and Body is set).
type Request struct {
	Method      string
	Path        string
	Query       url.Values
	Body        []byte
	ContentType string
	Accept      string
	Header      map[string]string
}

// Response is a successful (2xx) response.
type Response struct {
	Status int
	Header http.Header
	Body   []byte
}

// Do sends the request. It waits once on a 429 (Retry-After, at most 60 s) and slows down when the
// key is close to its per-minute limit, so a loop doesn't eat the whole minute.
func (c *Client) Do(r Request) (*Response, error) {
	if c.remaining >= 0 && c.remaining < 10 {
		c.Sleep(time.Duration(10-c.remaining) * 200 * time.Millisecond)
	}
	resp, err := c.send(r)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		wait := retryAfter(resp.Header.Get("Retry-After"))
		resp.Body.Close()
		c.Sleep(wait)
		if resp, err = c.send(r); err != nil {
			return nil, err
		}
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if n, err := strconv.Atoi(resp.Header.Get("X-RateLimit-Remaining")); err == nil {
		c.remaining = n
	}
	if resp.StatusCode >= 400 {
		return nil, decodeError(resp.StatusCode, body)
	}
	return &Response{Status: resp.StatusCode, Header: resp.Header, Body: body}, nil
}

func (c *Client) send(r Request) (*http.Response, error) {
	u := c.Base + r.Path
	if len(r.Query) > 0 {
		u += "?" + r.Query.Encode()
	}
	req, err := http.NewRequest(r.Method, u, bytes.NewReader(r.Body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("User-Agent", c.UserAgent)
	accept := r.Accept
	if accept == "" {
		accept = "application/json"
	}
	req.Header.Set("Accept", accept)
	if r.Body != nil {
		ct := r.ContentType
		if ct == "" {
			ct = "application/json"
		}
		req.Header.Set("Content-Type", ct)
	}
	for k, v := range r.Header {
		req.Header.Set(k, v)
	}
	return c.HTTP.Do(req)
}

func retryAfter(v string) time.Duration {
	s, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || s < 1 {
		s = 5
	}
	if s > 60 {
		s = 60
	}
	return time.Duration(s) * time.Second
}

func decodeError(status int, body []byte) *Error {
	e := &Error{Status: status, Body: body}
	var parsed struct {
		Message string `json:"message"`
		Code    string `json:"code"`
	}
	if json.Unmarshal(body, &parsed) == nil {
		e.Message, e.Code = parsed.Message, parsed.Code
	}
	if e.Message == "" {
		e.Message = http.StatusText(status)
	}
	return e
}

// JSON sends v (if not nil) as JSON and decodes the response into out (if not nil).
// It returns the raw body too, for --json output.
func (c *Client) JSON(method, path string, query url.Values, v, out any) ([]byte, error) {
	var body []byte
	if v != nil {
		var err error
		if body, err = json.Marshal(v); err != nil {
			return nil, err
		}
	}
	resp, err := c.Do(Request{Method: method, Path: path, Query: query, Body: body})
	if err != nil {
		return nil, err
	}
	if out != nil && len(resp.Body) > 0 {
		if err := json.Unmarshal(resp.Body, out); err != nil {
			return resp.Body, fmt.Errorf("unexpected response from %s %s: %w", method, path, err)
		}
	}
	return resp.Body, nil
}

// ---- Resources (fields as in the OpenAPI spec) ----

type Document struct {
	ID             int64   `json:"id"`
	FolderID       *int64  `json:"folder_id"`
	Title          string  `json:"title"`
	Slug           string  `json:"slug,omitempty"`
	Status         string  `json:"status,omitempty"`
	Position       int     `json:"position"`
	Content        *string `json:"content,omitempty"`
	WordCount      int     `json:"word_count"`
	UpdatedAt      string  `json:"updated_at"`
	LastAccessedAt *string `json:"last_accessed_at,omitempty"`
}

type Folder struct {
	ID       int64  `json:"id"`
	ParentID *int64 `json:"parent_id"`
	Name     string `json:"name"`
	Status   string `json:"status"`
	Position int    `json:"position"`
}

type Snapshot struct {
	ID        int64   `json:"id"`
	Kind      string  `json:"kind"`
	Name      string  `json:"name"`
	Title     string  `json:"title"`
	WordCount int     `json:"word_count"`
	Source    *string `json:"source"`
	TokenName *string `json:"token_name"`
	CreatedAt string  `json:"created_at"`
	Content   *string `json:"content,omitempty"`
}

type SearchResults struct {
	Folders []struct {
		ID     int64  `json:"id"`
		Name   string `json:"name"`
		Status string `json:"status"`
		Path   string `json:"path"`
	} `json:"folders"`
	Documents []struct {
		ID      int64   `json:"id"`
		Title   string  `json:"title"`
		Status  string  `json:"status"`
		Path    string  `json:"path"`
		Snippet *string `json:"snippet"`
	} `json:"documents"`
}

// Documents returns every document matching query, following cursors page by page.
func (c *Client) Documents(query url.Values) ([]Document, error) {
	q := url.Values{}
	for k, v := range query {
		q[k] = v
	}
	q.Set("limit", "500")
	var all []Document
	for {
		var page struct {
			Data []Document `json:"data"`
			Meta struct {
				NextCursor *string `json:"next_cursor"`
			} `json:"meta"`
		}
		if _, err := c.JSON("GET", "/documents", q, nil, &page); err != nil {
			return nil, err
		}
		all = append(all, page.Data...)
		if page.Meta.NextCursor == nil || *page.Meta.NextCursor == "" {
			return all, nil
		}
		q.Set("cursor", *page.Meta.NextCursor)
	}
}

// Folders returns every folder (projects are folders without a parent).
func (c *Client) Folders() ([]Folder, error) {
	var res struct {
		Data []Folder `json:"data"`
	}
	_, err := c.JSON("GET", "/folders", nil, nil, &res)
	return res.Data, err
}

// Content returns a document's raw Markdown and its lock value (X-Updated-At).
func (c *Client) Content(id int64) (string, string, error) {
	resp, err := c.Do(Request{Method: "GET", Path: fmt.Sprintf("/documents/%d/content", id), Accept: "text/markdown"})
	if err != nil {
		return "", "", err
	}
	return string(resp.Body), resp.Header.Get("X-Updated-At"), nil
}

// PutContent replaces a document's content. lock is the X-Updated-At from a read ("" = no check).
func (c *Client) PutContent(id int64, content, lock string) ([]byte, error) {
	h := map[string]string{}
	if lock != "" {
		h["X-Updated-At"] = lock
	}
	resp, err := c.Do(Request{
		Method: "PUT", Path: fmt.Sprintf("/documents/%d/content", id),
		Body: []byte(content), ContentType: "text/markdown; charset=utf-8", Header: h,
	})
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}
