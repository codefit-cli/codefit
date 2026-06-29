package cve

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// defaultOSVBaseURL is the public OSV.dev API. It is free and needs no API key.
const defaultOSVBaseURL = "https://api.osv.dev"

// osvClient implements Client against the OSV.dev REST API. It batches the
// affected-version lookup (one /v1/querybatch call for all dependencies) and then
// fetches each unique vulnerability's detail once (/v1/vulns/{id}).
type osvClient struct {
	baseURL string
	http    *http.Client
}

// OSVOption configures an osvClient.
type OSVOption func(*osvClient)

// WithBaseURL overrides the OSV API base URL (used by tests to point at a mock).
func WithBaseURL(u string) OSVOption {
	return func(c *osvClient) { c.baseURL = strings.TrimRight(u, "/") }
}

// WithHTTPClient overrides the HTTP client (timeouts, transport).
func WithHTTPClient(h *http.Client) OSVOption {
	return func(c *osvClient) { c.http = h }
}

// NewOSVClient returns a Client backed by OSV.dev.
func NewOSVClient(opts ...OSVOption) Client {
	c := &osvClient{
		baseURL: defaultOSVBaseURL,
		http:    &http.Client{Timeout: 30 * time.Second},
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// --- OSV wire types ---

type osvPackage struct {
	Name      string `json:"name"`
	Ecosystem string `json:"ecosystem"`
}

type osvQuery struct {
	Package osvPackage `json:"package"`
	Version string     `json:"version"`
}

type osvBatchRequest struct {
	Queries []osvQuery `json:"queries"`
}

// osvBatchResponse aligns with the request by index; querybatch returns only ids.
type osvBatchResponse struct {
	Results []struct {
		Vulns []struct {
			ID string `json:"id"`
		} `json:"vulns"`
	} `json:"results"`
}

// osvVuln is the detailed vulnerability record from /v1/vulns/{id}.
type osvVuln struct {
	ID       string `json:"id"`
	Summary  string `json:"summary"`
	Details  string `json:"details"`
	Severity []struct {
		Type  string `json:"type"`
		Score string `json:"score"`
	} `json:"severity"`
	Affected []struct {
		Package osvPackage `json:"package"`
		Ranges  []struct {
			Events []struct {
				Introduced string `json:"introduced"`
				Fixed      string `json:"fixed"`
			} `json:"events"`
		} `json:"ranges"`
	} `json:"affected"`
	References []struct {
		Type string `json:"type"`
		URL  string `json:"url"`
	} `json:"references"`
	DatabaseSpecific map[string]any `json:"database_specific"`
}

// Query asks OSV.dev which of deps have known vulnerabilities and returns them
// keyed by "name@version". It does one batched query, then fetches each distinct
// vulnerability's detail once (cached). A non-2xx response or a result/req length
// mismatch is a loud error — codefit never silently reports "no vulnerabilities".
func (c *osvClient) Query(ctx context.Context, deps []Dependency) (map[string][]Vulnerability, error) {
	out := map[string][]Vulnerability{}
	if len(deps) == 0 {
		return out, nil
	}

	reqBody := osvBatchRequest{Queries: make([]osvQuery, len(deps))}
	for i, d := range deps {
		reqBody.Queries[i] = osvQuery{
			Package: osvPackage{Name: d.Name, Ecosystem: d.Ecosystem},
			Version: osvQueryVersion(d),
		}
	}
	var batch osvBatchResponse
	if err := c.postJSON(ctx, "/v1/querybatch", reqBody, &batch); err != nil {
		return nil, err
	}
	if len(batch.Results) != len(deps) {
		return nil, fmt.Errorf("osv querybatch: %d results for %d queries (the API contract is index-aligned)", len(batch.Results), len(deps))
	}

	cache := map[string]Vulnerability{}
	for i, d := range deps {
		for _, ref := range batch.Results[i].Vulns {
			vuln, ok := cache[ref.ID]
			if !ok {
				detail, err := c.fetchVuln(ctx, ref.ID)
				if err != nil {
					return nil, err
				}
				vuln = detail
				cache[ref.ID] = vuln
			}
			key := d.Name + "@" + d.Version
			out[key] = append(out[key], vuln)
		}
	}
	return out, nil
}

// osvQueryVersion returns the version in the form OSV expects per ecosystem. Go
// module versions are "vX.Y.Z" in go.mod but OSV's Go ecosystem compares them
// without the leading "v"; npm versions are used as-is.
func osvQueryVersion(d Dependency) string {
	if d.Ecosystem == "Go" {
		return strings.TrimPrefix(d.Version, "v")
	}
	return d.Version
}

// fetchVuln retrieves and maps a single vulnerability's detail.
func (c *osvClient) fetchVuln(ctx context.Context, id string) (Vulnerability, error) {
	var v osvVuln
	if err := c.getJSON(ctx, "/v1/vulns/"+id, &v); err != nil {
		return Vulnerability{}, err
	}
	summary := v.Summary
	if summary == "" {
		summary = v.Details
	}
	refs := make([]string, 0, len(v.References))
	for _, r := range v.References {
		if r.URL != "" {
			refs = append(refs, r.URL)
		}
	}
	return Vulnerability{
		ID:         v.ID,
		Summary:    summary,
		Severity:   osvSeverity(v),
		FixedIn:    osvFixedIn(v),
		References: refs,
	}, nil
}

// osvSeverity reports OSV's severity WITHOUT recomputing a CVSS score (codefit
// surfaces, it does not score): the GHSA-style database_specific label first,
// else the first CVSS vector string, else "UNKNOWN".
func osvSeverity(v osvVuln) string {
	if s, ok := v.DatabaseSpecific["severity"].(string); ok && s != "" {
		return s
	}
	if len(v.Severity) > 0 && v.Severity[0].Score != "" {
		return v.Severity[0].Score
	}
	return "UNKNOWN"
}

// osvFixedIn returns the first "fixed" version found across the affected ranges,
// or "" when OSV declares no fix.
func osvFixedIn(v osvVuln) string {
	for _, a := range v.Affected {
		for _, r := range a.Ranges {
			for _, e := range r.Events {
				if e.Fixed != "" {
					return e.Fixed
				}
			}
		}
	}
	return ""
}

func (c *osvClient) postJSON(ctx context.Context, path string, body, dst any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("encoding osv request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(b))
	if err != nil {
		return fmt.Errorf("building osv request %s: %w", path, err)
	}
	req.Header.Set("Content-Type", "application/json")
	return c.do(req, path, dst)
}

func (c *osvClient) getJSON(ctx context.Context, path string, dst any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("building osv request %s: %w", path, err)
	}
	return c.do(req, path, dst)
}

func (c *osvClient) do(req *http.Request, path string, dst any) error {
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("osv request %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("osv %s: unexpected status %d", path, resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(dst); err != nil {
		return fmt.Errorf("decoding osv response from %s: %w", path, err)
	}
	return nil
}
