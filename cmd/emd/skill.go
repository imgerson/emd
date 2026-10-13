package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	assets "github.com/imgerson/emd"
)

// cmdSkill prints the agent skill, or installs it as a personal Claude Code skill.
func cmdSkill(a *app, args []string) error {
	fs := flag.NewFlagSet("skill", flag.ContinueOnError)
	dir := fs.String("dir", "", "skills directory (default ~/.claude/skills)")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) == 0 {
		_, err := a.out.Write(assets.Skill)
		return err
	}
	if len(pos) != 1 || pos[0] != "install" {
		return usagef("emd skill [install [--dir DIR]]")
	}
	d := *dir
	if d == "" {
		home := a.getenv("HOME")
		if !filepath.IsAbs(home) {
			return errors.New("HOME is not set: pass --dir")
		}
		d = filepath.Join(home, ".claude", "skills")
	}
	p := filepath.Join(d, "easymarkdown-plans", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(p, assets.Skill, 0o644); err != nil {
		return err
	}
	fmt.Fprintln(a.out, "Installed the easymarkdown-plans skill:", p)
	return nil
}
