package scaffold

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"

	"github.com/codefit-cli/codefit/internal/config"
	"github.com/codefit-cli/codefit/internal/providers"
	"github.com/codefit-cli/codefit/internal/providers/registry"
)

// ProjectInfo is the deterministic picture of a project that drives config and
// skill generation. Every field is inferred from files on disk — never an LLM.
type ProjectInfo struct {
	Name            string // the project directory's base name
	Language        string // typescript | go | python | java
	Framework       string // a value within config.allowedFrameworks, or ""
	ORM             string // prisma | drizzle | typeorm | ""
	DBType          string // postgresql | mysql | sqlite | "" (within config.allowedDBTypes)
	DBParadigm      string // oltp | olap | mixed | "" (auto when a DB is detected, seeding paradigm detection)
	SchemaPaths     []string
	RouteHandlers   int // count of route handlers found (informational, for the report)
	PathCriticality config.PathCriticality
}

// dirsToSkip are never walked when counting route handlers — vendored or built
// output, not the project's own source.
var dirsToSkip = map[string]bool{
	"node_modules": true, ".git": true, "dist": true, "build": true,
	".next": true, "vendor": true, "out": true,
}

// Detect inspects root and infers the project's language and stack from marker
// files. It returns an error when no supported language can be identified — init
// must not write a config it cannot stand behind.
func Detect(root string) (ProjectInfo, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return ProjectInfo{}, fmt.Errorf("resolving project root %q: %w", root, err)
	}

	info := ProjectInfo{Name: filepath.Base(abs)}

	provider := detectLanguage(root)
	if provider == nil {
		return ProjectInfo{}, fmt.Errorf(
			"no supported language detected in %q: expected one of go.mod, package.json, "+
				"pyproject.toml/requirements.txt, or pom.xml/build.gradle", root)
	}
	info.Language = provider.Language()
	info.PathCriticality = provider.DefaultPathCriticality()

	// Route-handler enrichment is a TypeScript/Next concern (route.ts files); for
	// other languages it would be a wasted full-tree walk that can only count zero.
	if info.Language == "typescript" {
		enrichTypeScript(root, &info)
		info.RouteHandlers = countRouteHandlers(root)
	}
	return info, nil
}

// detectLanguage resolves the project's language provider from marker files,
// via internal/providers/registry.ByMarkerFile — the registry's table order
// IS the priority (go.mod wins over package.json in a polyglot root; a
// monorepo with separate per-language sub-projects should run init per
// sub-project root rather than at the polyglot root). Only TypeScript and Go
// are registered today; Python and Java markers are recognized elsewhere
// (readPackageDeps et al.) but unsupported, so they fall through to nil.
func detectLanguage(root string) providers.LanguageProvider {
	e, ok := registry.ByMarkerFile(root)
	if !ok {
		return nil
	}
	return e.New(nil)
}

// enrichTypeScript fills framework, ORM and database from a TypeScript project's
// package.json and Prisma schema, and widens path_criticality for the framework's
// route location (Next.js handlers live under app/, which the src/** default misses).
func enrichTypeScript(root string, info *ProjectInfo) {
	deps := readPackageDeps(root)
	info.Framework = frameworkFromDeps(deps)

	if info.Framework == "next" && !slices.Contains(info.PathCriticality.Production, "app/**") {
		info.PathCriticality.Production = append(
			[]string{"app/**"}, info.PathCriticality.Production...)
	}

	if rel := findPrismaSchema(root); rel != "" {
		info.ORM = "prisma"
		info.SchemaPaths = []string{rel}
		info.DBParadigm = "auto"
		info.DBType = prismaProvider(filepath.Join(root, rel))
	} else if _, ok := deps["drizzle-orm"]; ok {
		info.ORM = "drizzle"
	} else if _, ok := deps["typeorm"]; ok {
		info.ORM = "typeorm"
	}
}

// frameworkFromDeps maps package.json dependencies to a framework recognized by
// config validation. next implies react, so it wins; nestjs is intentionally not
// emitted because it is not in config.allowedFrameworks (emitting it would produce
// an invalid .codefit.yaml).
func frameworkFromDeps(deps map[string]string) string {
	switch {
	case has(deps, "next"):
		return "next"
	case has(deps, "express"):
		return "express"
	case has(deps, "react"):
		return "react"
	default:
		return ""
	}
}

// readPackageDeps returns the merged dependencies + devDependencies of the
// project's package.json (empty map when absent or unparseable).
func readPackageDeps(root string) map[string]string {
	data, err := os.ReadFile(filepath.Join(root, "package.json"))
	if err != nil {
		return map[string]string{}
	}
	var pkg struct {
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return map[string]string{}
	}
	merged := make(map[string]string, len(pkg.Dependencies)+len(pkg.DevDependencies))
	for k, v := range pkg.Dependencies {
		merged[k] = v
	}
	for k, v := range pkg.DevDependencies {
		merged[k] = v
	}
	return merged
}

// findPrismaSchema returns the project-relative path of prisma/schema.prisma if
// present, in slash-normalized form for the config; "" otherwise.
func findPrismaSchema(root string) string {
	rel := filepath.Join("prisma", "schema.prisma")
	if exists(root, rel) {
		return filepath.FromSlash(rel)
	}
	return ""
}

// prismaDatasourceProvider extracts the datasource provider from a Prisma schema.
var prismaDatasourceProvider = regexp.MustCompile(`provider\s*=\s*"([a-zA-Z]+)"`)

// prismaProvider reads the datasource provider from a Prisma schema and maps it
// to a config-allowed DB type. Unknown providers (cockroachdb, mongodb, …) return
// "" rather than an invalid value.
func prismaProvider(schemaPath string) string {
	data, err := os.ReadFile(schemaPath)
	if err != nil {
		return ""
	}
	// The first provider in the file is the datasource's (the generator's
	// provider is prisma-client-js, which the allow-list below excludes).
	for _, m := range prismaDatasourceProvider.FindAllStringSubmatch(string(data), -1) {
		switch m[1] {
		case "postgresql", "postgres":
			return "postgresql"
		case "mysql":
			return "mysql"
		case "sqlite":
			return "sqlite"
		}
	}
	return ""
}

// countRouteHandlers counts files named route.ts/route.tsx (the Next.js App
// Router convention) under root, skipping vendored and build directories. It is
// informational only — codefit's scanner discovers handlers itself at scan time.
func countRouteHandlers(root string) int {
	count := 0
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // skip unreadable entries, keep counting
		}
		if d.IsDir() {
			if dirsToSkip[d.Name()] {
				return fs.SkipDir
			}
			return nil
		}
		if base := d.Name(); base == "route.ts" || base == "route.tsx" {
			count++
		}
		return nil
	})
	return count
}

func exists(root, rel string) bool {
	_, err := os.Stat(filepath.Join(root, rel))
	return err == nil
}

func has(deps map[string]string, name string) bool {
	_, ok := deps[name]
	return ok
}
