package scaffold

import (
	"fmt"
	"os"
	"path/filepath"
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

	if res.ConfigAction, err = writeConfig(opts, info); err != nil {
		return Result{}, err
	}
	if res.Skills, res.UsedFallback, err = placeSkill(opts.Root, info); err != nil {
		return Result{}, err
	}
	return res, nil
}

// writeConfig renders and writes .codefit.yaml, respecting the overwrite decision.
func writeConfig(opts Options, info ProjectInfo) (ConfigAction, error) {
	path := filepath.Join(opts.Root, ConfigName)
	existed := fileExists(path)
	if existed && !opts.OverwriteConfig {
		return ConfigSkipped, nil
	}
	data, err := RenderConfig(info)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", fmt.Errorf("writing %s: %w", ConfigName, err)
	}
	if existed {
		return ConfigOverwritten, nil
	}
	return ConfigCreated, nil
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
