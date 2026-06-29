package cve

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestOSVClient_Query drives the client against a mock OSV server (never the real
// api.osv.dev): querybatch returns vuln ids per query by index, and vulns/{id}
// returns the detail fixture. It asserts the matched dep carries the vuln with the
// OSV-provided severity, fixed version and references; a clean dep carries none;
// and the Go version is sent without its "v" prefix.
func TestOSVClient_Query(t *testing.T) {
	var gotQueries []osvQuery
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/querybatch":
			var req osvBatchRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			gotQueries = req.Queries
			parts := make([]string, len(req.Queries))
			for i, q := range req.Queries {
				if q.Package.Name == "lodash" {
					parts[i] = `{"vulns":[{"id":"GHSA-jf85-cpcp-j695"}]}`
				} else {
					parts[i] = `{"vulns":[]}`
				}
			}
			fmt.Fprintf(w, `{"results":[%s]}`, strings.Join(parts, ","))
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/vulns/"):
			_, _ = w.Write(readFixture(t, "osv_vuln_lodash.json"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	client := NewOSVClient(WithBaseURL(srv.URL))
	deps := []Dependency{
		{Name: "lodash", Version: "4.17.4", Ecosystem: "npm"},
		{Name: "express", Version: "4.18.2", Ecosystem: "npm"},
		{Name: "github.com/foo/bar", Version: "v1.2.3", Ecosystem: "Go"},
	}
	res, err := client.Query(context.Background(), deps)
	if err != nil {
		t.Fatal(err)
	}

	vulns := res["lodash@4.17.4"]
	if len(vulns) != 1 {
		t.Fatalf("lodash should have 1 vuln, got %d: %+v", len(vulns), res)
	}
	v := vulns[0]
	if v.ID != "GHSA-jf85-cpcp-j695" {
		t.Errorf("vuln id: got %q", v.ID)
	}
	if v.Severity != "HIGH" {
		t.Errorf("severity from database_specific: got %q, want HIGH", v.Severity)
	}
	if v.FixedIn != "4.17.5" {
		t.Errorf("fixed version from affected ranges: got %q, want 4.17.5", v.FixedIn)
	}
	if len(v.References) == 0 {
		t.Errorf("references must be carried, got none")
	}
	if len(res["express@4.18.2"]) != 0 {
		t.Errorf("express is clean, must carry no vulns: %+v", res["express@4.18.2"])
	}

	for _, q := range gotQueries {
		if q.Package.Ecosystem == "Go" && q.Version != "1.2.3" {
			t.Errorf("Go version must be sent without the v prefix, got %q", q.Version)
		}
	}
}
