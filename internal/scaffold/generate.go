package scaffold

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/codefit-cli/codefit/internal/config"
)

// ConfigName is the project config file codefit reads and init writes.
const ConfigName = ".codefit.yaml"

// ConfigAction reports what Generate did with the project config.
type ConfigAction string

const (
	ConfigCreated     ConfigAction = "created"     // no config existed; written
	ConfigOverwritten ConfigAction = "overwritten" // existed; replaced on permission
	ConfigSkipped     ConfigAction = "skipped"     // existed; left untouched
)

// Options controls a Generate run.
type Options struct {
	Root            string
	OverwriteConfig bool // when .codefit.yaml exists, replace it (the dev's decision)
}

// SkillWrite records one skill file placement, for the init report.
type SkillWrite struct {
	Agent string // the agent whose location it serves
	Path  string // project-relative path written
}

// Result is the full account of what Generate did, for the caller to report —
// nothing is written silently.
type Result struct {
	Info         ProjectInfo
	ConfigPath   string // project-relative
	ConfigAction ConfigAction
	UsedFallback bool // skill placed in the standard location (no agent detected)
	Skills       []SkillWrite
	// DroppedSchemaPaths names every database.schema_paths entry the PREVIOUS
	// config carried that this run did not write again, with the reason it no
	// longer proves.
	//
	// It exists because `init --force` re-derives schema_paths from disk, and a
	// path that stops proving therefore disappears. A silent disappearance is
	// found out from a later scan that suddenly measures nothing, with nothing
	// connecting it to the init that caused it — the developer decides, but only
	// if codefit tells them what changed.
	DroppedSchemaPaths []SchemaCandidate
}

// Generate runs the full init: detect the project, write .codefit.yaml (honoring
// the overwrite decision), and place codefit's skill for every detected agent.
//
// The skill is ALWAYS (re)written — codefit owns it. Only the config is gated on
// permission, because it is shared, user-owned project config. Generate never
// prompts: the caller resolves OverwriteConfig and reports the Result.
func Generate(opts Options) (Result, error) {
	info, err := Detect(opts.Root)
	if err != nil {
		return Result{}, err
	}
	res := Result{Info: info, ConfigPath: ConfigName}

	if res.ConfigAction, res.DroppedSchemaPaths, err = writeConfig(opts, info); err != nil {
		return Result{}, err
	}
	if res.Skills, res.UsedFallback, err = placeSkill(opts.Root, info); err != nil {
		return Result{}, err
	}
	return res, nil
}

// writeConfig renders and writes .codefit.yaml, respecting the overwrite
// decision, and reports any schema_paths entry the file it replaced carried and
// this one does not.
//
// The demotion is read from the OUTGOING file, before it is overwritten, because
// afterwards the fact is gone. It is deliberately not recomputed from disk a
// second time: what the developer needs to know is what CHANGED between the
// config they had and the config they now have.
func writeConfig(opts Options, info ProjectInfo) (ConfigAction, []SchemaCandidate, error) {
	path := filepath.Join(opts.Root, ConfigName)
	existed := fileExists(path)
	if existed && !opts.OverwriteConfig {
		return ConfigSkipped, nil, nil
	}
	var dropped []SchemaCandidate
	if existed {
		dropped = droppedSchemaPaths(path, info)
	}
	data, err := RenderConfig(info)
	if err != nil {
		return "", nil, err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", nil, fmt.Errorf("writing %s: %w", ConfigName, err)
	}
	// Backstop the honesty rule: never report a config as written if it does not
	// load+validate. Values come from validated enums and the name is YAML-escaped,
	// so this should never fire — but a success message over a broken file would be
	// exactly the silent failure the project forbids.
	if _, err := config.Load(path); err != nil {
		return "", nil, fmt.Errorf("generated %s failed validation: %w", ConfigName, err)
	}
	if existed {
		return ConfigOverwritten, dropped, nil
	}
	return ConfigCreated, dropped, nil
}

// droppedSchemaPaths compares the schema_paths of the config about to be
// replaced against the one about to be written, and returns what is going away
// with the reason discovery gave for it.
//
// An unloadable existing config yields nothing rather than an error: it is a
// file the developer hand-edited into an invalid state, and refusing to
// regenerate over it would take away the one command that fixes it.
func droppedSchemaPaths(path string, info ProjectInfo) []SchemaCandidate {
	prev, err := config.Load(path)
	if err != nil {
		return nil
	}
	var out []SchemaCandidate
	for _, was := range prev.Database.SchemaPaths {
		p := filepath.ToSlash(was)
		if slices.Contains(toSlashAll(info.SchemaPaths), p) {
			continue
		}
		out = append(out, SchemaCandidate{Path: p, Reason: dropReason(info, p)})
	}
	return out
}

// dropReason explains a demotion with the measurement discovery actually made of
// that path when it still exists, and says plainly that it is gone when it does
// not. A reason invented here would be the guess this whole change removes.
func dropReason(info ProjectInfo, path string) string {
	for _, c := range info.SchemaCandidates {
		if c.Path == path {
			return c.Reason
		}
	}
	return "no longer holds SQL files codefit can order and reconstruct"
}

// placeSkill renders codefit's skill and writes it to every placement target,
// de-duplicating identical destinations.
func placeSkill(root string, info ProjectInfo) (writes []SkillWrite, usedFallback bool, err error) {
	data, err := RenderSkill(info)
	if err != nil {
		return nil, false, err
	}
	targets, usedFallback := PlacementTargets(root)
	seen := make(map[string]bool, len(targets))
	for _, tgt := range targets {
		if seen[tgt.SkillDir] {
			continue
		}
		seen[tgt.SkillDir] = true

		dir := filepath.Join(root, tgt.SkillDir)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, usedFallback, fmt.Errorf("creating skill dir %s: %w", tgt.SkillDir, err)
		}
		rel := filepath.Join(tgt.SkillDir, SkillFileName)
		if err := os.WriteFile(filepath.Join(root, rel), data, 0o644); err != nil {
			return nil, usedFallback, fmt.Errorf("writing skill %s: %w", rel, err)
		}
		writes = append(writes, SkillWrite{Agent: tgt.Name, Path: rel})
	}
	return writes, usedFallback, nil
}

// ConfigExists reports whether root already has a .codefit.yaml — the caller
// uses it to decide whether to ask before regenerating.
func ConfigExists(root string) bool {
	return fileExists(filepath.Join(root, ConfigName))
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
