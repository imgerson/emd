// Package emd holds the files the emd binary ships with.
package emd

import _ "embed"

// Skill is the easymarkdown-plans agent skill, also published as the Claude Code plugin
// (plugins/emd). `emd skill` prints it and `emd skill install` writes it.
//
//go:embed plugins/emd/skills/easymarkdown-plans/SKILL.md
var Skill []byte
