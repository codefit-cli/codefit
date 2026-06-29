package mcp

import (
	"context"
	"fmt"

	"github.com/codefit-cli/codefit/internal/core/cve"
)

// CheckCVEsRequest is the input to codefit-check-cves: a project root. The
// dependency manifests are auto-detected under it.
type CheckCVEsRequest struct {
	Root string `json:"root"`
}

// VulnerableDependency is one dependency that OSV.dev reports as vulnerable, with
// every vulnerability affecting its exact installed version.
type VulnerableDependency struct {
	Name            string              `json:"name"`
	Version         string              `json:"version"`
	Ecosystem       string              `json:"ecosystem"`
	Vulnerabilities []cve.Vulnerability `json:"vulnerabilities"`
}

// CheckCVEsResponse reports the vulnerable dependencies, how many were scanned,
// and any honest notes (a manifest present without its lockfile, or none found).
type CheckCVEsResponse struct {
	Vulnerable          []VulnerableDependency `json:"vulnerable"`
	DependenciesScanned int                    `json:"dependencies_scanned"`
	Notes               []string               `json:"notes"`
}

// osvClientFactory builds the CVE client. A package seam so tests inject a mock
// without touching the network; it defaults to the real OSV.dev client.
var osvClientFactory = func() cve.Client { return cve.NewOSVClient() }

// swapCVEClient replaces the CVE client factory and returns a restore func (test
// seam — keeps osvClientFactory unexported).
func swapCVEClient(factory func() cve.Client) (restore func()) {
	prev := osvClientFactory
	osvClientFactory = factory
	return func() { osvClientFactory = prev }
}

// HandleCheckCVEs reads the project's dependency manifests (exact versions from
// lockfiles / go.mod only), queries OSV.dev for known vulnerabilities, and returns
// the vulnerable dependencies. A thin adapter: it parses manifests, queries the
// client, and reshapes the result — no detection logic here. When no version
// resolves (no lockfile), it returns the honest note WITHOUT querying OSV.
func HandleCheckCVEs(req CheckCVEsRequest) (CheckCVEsResponse, error) {
	scan, err := cve.ParseManifests(req.Root)
	if err != nil {
		return CheckCVEsResponse{}, err
	}
	resp := CheckCVEsResponse{DependenciesScanned: len(scan.Dependencies), Notes: scan.Notes}
	if len(scan.Dependencies) == 0 {
		return resp, nil
	}
	vulns, err := osvClientFactory().Query(context.Background(), scan.Dependencies)
	if err != nil {
		return CheckCVEsResponse{}, fmt.Errorf("querying OSV.dev: %w", err)
	}
	for _, d := range scan.Dependencies {
		if vs := vulns[d.Name+"@"+d.Version]; len(vs) > 0 {
			resp.Vulnerable = append(resp.Vulnerable, VulnerableDependency{
				Name:            d.Name,
				Version:         d.Version,
				Ecosystem:       d.Ecosystem,
				Vulnerabilities: vs,
			})
		}
	}
	return resp, nil
}
