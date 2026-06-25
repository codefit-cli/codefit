// Package scaffold powers `codefit init`: it inspects a project on disk and
// produces the artifacts that make codefit usable from an AI agent.
//
// It does three jobs, all deterministic and LLM-free:
//
//  1. Detect — read marker files (go.mod, package.json, prisma/schema.prisma, …)
//     to infer language, framework, ORM and database, yielding a [ProjectInfo].
//  2. Render — turn that ProjectInfo into a commented .codefit.yaml (committed,
//     shared project config) and codefit's own thin SKILL.md.
//  3. Place — write the skill where each detected agent (Claude Code, OpenCode,
//     Codex) discovers it, declaring every path it touched.
//
// scaffold sits ABOVE the language providers (like the MCP adapter): it is the
// single place that maps a filesystem to a language. The core stays agnostic.
package scaffold
