// Package fixture is the registry's minimal per-language corpus item for "go"
// (internal/providers/registry/surfacecontract_test.go): a single real HTTP
// handler, guaranteed to produce at least one "authz" surface item through the
// real go/ast-backed provider.
package fixture

import "net/http"

func DeleteWidget(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}
