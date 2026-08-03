// Package scope is layer 0 of the filtering pyramid (PRD section 19), as a
// value: WHICH project-relative files an audit pass is allowed to look at.
//
// It is a pure leaf — it imports nothing from codefit — so the core, the sensors
// and the MCP adapter can all speak the same narrowing without a dependency.
//
// codefit does not derive the scope from git. It never reads .git, never diffs
// refs and assumes no branch model: it has no power over the user's git
// (CLAUDE.md, autonomy principle), and the MCP caller is an agent that already
// knows which files it touched. The scope is therefore an INPUT.
//
// Two fail-safes point in opposite directions, on purpose:
//
//   - [Of] with an absent or empty list returns [Full]. "Nothing was passed" must
//     never be read as "audit nothing"; the safe direction for an auditor is to
//     look at MORE.
//   - The zero value Scope{} includes NOTHING, and that is why [Full] is an
//     explicit constructor rather than the zero value. baseline.Diff uses
//     [Scope.Includes] to decide whether an item may be marked gone (and later
//     pruned), so a caller that forgets to pass a scope prunes nothing —
//     under-reporting instead of deleting audit memory it never verified.
//
// A walk wants the third question, not either of those: does this scope restrict
// me at all? That is [Scope.Narrows], false for both Full{} and the zero value,
// so an unset scope can never silently blank a walk.
package scope
