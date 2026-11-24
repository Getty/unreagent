// Tests for what `unreagent init` sets up in the project itself (k256): the
// starter CLAUDE.md, skills.materialize in the config it writes, the files a
// start writes (skills, topic agents, rules file) written right away, the
// findings on them, the hold before a first sweep over unreagent-* skill
// directories unreagent cannot tell are its own, and -check writing nothing.
// No test here depends on how many skills or agents are shipped.
package main

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/conflict-industries/unreagent/internal/config"
	"github.com/conflict-industries/unreagent/internal/skills"
)

// snapshot is every entry below root, by slash path: its type and mode, a
// file's content, a link's target and, with times, a file's modification
// time — so that a rewrite with the same bytes shows as well.
func snapshot(t *testing.T, root string, times bool) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		fi, err := os.Lstat(p)
		if err != nil {
			return err
		}
		v := fi.Mode().String()
		switch {
		case fi.Mode()&fs.ModeSymlink != 0:
			target, _ := os.Readlink(p)
			v += " -> " + target
		case fi.Mode().IsRegular():
			v += " " + mustReadFile(t, p)
			if times {
				v += fmt.Sprintf(" @%d", fi.ModTime().UnixNano())
			}
		}
		out[filepath.ToSlash(rel)] = v
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	return out
}

// sameSnapshot reports every path that differs between two snapshots.
func sameSnapshot(t *testing.T, what string, before, after map[string]string) {
	t.Helper()
	for p, v := range after {
		if b, ok := before[p]; !ok {
			t.Errorf("%s: %s appeared", what, p)
		} else if b != v {
			t.Errorf("%s: %s changed", what, p)
		}
	}
	for p := range before {
		if _, ok := after[p]; !ok {
			t.Errorf("%s: %s vanished", what, p)
		}
	}
}

// cleanTree is a project with everything a start needs, so that the
// preflight has no warning of its own.
func cleanTree(t *testing.T) initTree {
	t.Helper()
	tr := newInitTree(t, buildVersion584, "node", "npm", "claude")
	tr.toolkit(`{"FriendlyName": "UE LLM Toolkit"}`, true)
	tr.built()
	return tr
}

func (tr initTree) claude() string   { return filepath.Join(tr.dir, ".claude") }
func (tr initTree) skills() string   { return filepath.Join(tr.dir, ".claude", "skills") }
func (tr initTree) claudeMD() string { return filepath.Join(tr.dir, "CLAUDE.md") }

// wantSetUp checks that the project holds what a start with skills and agents
// on writes: every shipped skill, every agent file and the rules file, as
// rendered from the shipped set.
func wantSetUp(t *testing.T, tr initTree) {
	t.Helper()
	all := shippedSkills(t)
	got := tree(t, tr.skills())
	for p, want := range materialized(all) {
		if got[p] != want {
			t.Errorf("skill file %s is not the materialized one", p)
		}
	}
	set := shippedAgentSet(t)
	for _, a := range set.Agents {
		if got := readOrEmpty(agentPath(tr.claude(), a.Name)); got != renderAgent(a) {
			t.Errorf("agent file for %s is not the rendered one", a.Name)
		}
	}
	if got := readOrEmpty(rulesPath(tr.claude())); got != renderRules(set) {
		t.Errorf("rules file is not the rendered one:\n%s", got)
	}
}

// wantNoSetupWarn fails on a warning about CLAUDE.md, skills or agents. The
// hand-written configs here have no toolkit entry, so the preflight warns
// about that one.
func wantNoSetupWarn(t *testing.T, out string) {
	t.Helper()
	wantNone(t, out, "[WARN]     CLAUDE.md", "[WARN]     skills", "[WARN]     agent")
}

func readOrEmpty(path string) string {
	b, _ := os.ReadFile(path)
	return string(b)
}

// A fresh project — no config, no CLAUDE.md — is complete after one init: a
// config that materializes, the starter CLAUDE.md with its kickstart block,
// the skills, the agents and the rules file. A second init changes nothing,
// and -check after it writes nothing.
func TestInitSetsUpAFreshProject(t *testing.T) {
	tr := cleanTree(t)
	out, err := tr.run(false)
	if err != nil {
		t.Fatalf("init failed: %v\n%s", err, out)
	}
	if got := mustReadFile(t, tr.config()); !strings.Contains(got, "\nskills:\n  materialize: true\n") {
		t.Errorf("written config does not turn materialize on:\n%s", got)
	}
	set := shippedAgentSet(t)
	if got := mustReadFile(t, tr.claudeMD()); got != set.Starter {
		t.Errorf("CLAUDE.md is not the starter:\n%s", got)
	}
	if fi, err := os.Stat(tr.claudeMD()); err == nil && fi.Mode().Perm() != 0o644&^currentUmask() {
		t.Errorf("CLAUDE.md mode = %v, want that of the config init writes", fi.Mode().Perm())
	}
	wantSetUp(t, tr)
	wantAll(t, out,
		"Wrote "+tr.config(),
		"Wrote "+tr.claudeMD()+": the starter CLAUDE.md.",
		"[info]     CLAUDE.md: "+tr.claudeMD()+" still holds the kickstart block",
		"[ok]       skills: ", "materialized into "+tr.skills(),
		"[ok]       agents: ", "in "+tr.claude()+": ",
		"0 blocking, 0 warning(s)")

	before := snapshot(t, tr.dir, false)
	out, err = tr.run(false)
	if err != nil {
		t.Fatalf("second init failed: %v\n%s", err, out)
	}
	sameSnapshot(t, "second init", before, snapshot(t, tr.dir, false))
	wantAll(t, out, "exists and is left as it is.", "still holds the kickstart block", "0 blocking, 0 warning(s)")
	wantNone(t, out, "Wrote "+tr.claudeMD(), "Wrote "+tr.config())

	before = snapshot(t, tr.dir, true)
	out, err = tr.run(true)
	if err != nil {
		t.Fatalf("init -check failed: %v\n%s", err, out)
	}
	sameSnapshot(t, "init -check", before, snapshot(t, tr.dir, true))
	wantAll(t, out, "agents: `unreagent init` writes ", " 0 new, 0 refreshed, ")
}

// currentUmask is the process umask, which os.OpenFile's mode passes through.
func currentUmask() os.FileMode {
	dir, err := os.MkdirTemp("", "umask")
	if err != nil {
		return 0
	}
	defer os.RemoveAll(dir)
	p := filepath.Join(dir, "f")
	if err := os.WriteFile(p, nil, 0o777); err != nil {
		return 0
	}
	fi, err := os.Stat(p)
	if err != nil {
		return 0
	}
	return 0o777 &^ fi.Mode().Perm()
}

// A CLAUDE.md the project has — at the top or in .claude/ — is the user's:
// left byte for byte, no second one written, and the finding says whether the
// kickstart block is still in it (both marker lines, each alone on its line).
func TestInitLeavesTheProjectsCLAUDEmd(t *testing.T) {
	starter := shippedAgentSet(t).Starter
	removed := strings.Replace(starter, skills.KickstartBegin, "", 1)
	for name, c := range map[string]struct {
		path, content, want string
	}{
		"own file":           {"CLAUDE.md", "# Mine\r\n\r\nNo block here.\n", "is there, without the kickstart block"},
		"in .claude":         {".claude/CLAUDE.md", "# Mine\n", "is there, without the kickstart block"},
		"kickstart still in": {"CLAUDE.md", starter, "still holds the kickstart block"},
		"kickstart removed":  {"CLAUDE.md", removed, "is there, without the kickstart block"},
		"CRLF kickstart":     {"CLAUDE.md", strings.ReplaceAll(starter, "\n", "\r\n"), "still holds the kickstart block"},
	} {
		t.Run(name, func(t *testing.T) {
			tr := cleanTree(t)
			path := filepath.Join(tr.dir, filepath.FromSlash(c.path))
			mustWriteFile(t, path, c.content)
			for _, check := range []bool{true, false} {
				out, err := tr.run(check)
				if err != nil {
					t.Fatalf("init (check %v) failed: %v\n%s", check, err, out)
				}
				wantAll(t, out, "CLAUDE.md: "+path+" "+c.want)
				wantNone(t, out, "Wrote "+tr.claudeMD(), "CLAUDE.md: none")
			}
			if got := mustReadFile(t, path); got != c.content {
				t.Errorf("%s was changed:\n%q", c.path, got)
			}
			if c.path != "CLAUDE.md" && fileExists(tr.claudeMD()) {
				t.Error("init wrote a CLAUDE.md next to the one in .claude/")
			}
		})
	}
}

// An existing config is never edited. With skills.materialize off in it, no
// skills, agents or rules file are written, and the finding gives the lines to
// add and the file to add them to; the starter CLAUDE.md is written all the
// same.
func TestInitExistingConfigWithoutMaterialize(t *testing.T) {
	const existing = "# mine\nagent: {enabled: true}\nmcp: {enabled: true}\n"
	for _, local := range []bool{false, true} {
		t.Run(fmt.Sprintf("local overlay %v", local), func(t *testing.T) {
			tr := cleanTree(t)
			mustWriteFile(t, tr.config(), existing)
			if local {
				mustWriteFile(t, filepath.Join(tr.dir, "unreagent.local.yaml"), "agent: {enabled: true}\n")
			}
			out, err := tr.run(false)
			if err != nil {
				t.Fatalf("init failed: %v\n%s", err, out)
			}
			if got := mustReadFile(t, tr.config()); got != existing {
				t.Errorf("unreagent.yaml was changed:\n%s", got)
			}
			if fileExists(tr.claude()) {
				t.Errorf("init wrote into .claude/ with materialize off: %v", snapshot(t, tr.claude(), false))
			}
			if !fileExists(tr.claudeMD()) {
				t.Error("init wrote no starter CLAUDE.md")
			}
			wantAll(t, out,
				"[info]     skills: not written into the project — skills.materialize is off, so no skills, topic agents or rules file are written there",
				"add to unreagent.yaml (into its skills: section, if it has one):\n",
				"                 skills:\n                   materialize: true\n",
				"not in unreagent.local.yaml")
			wantNoSetupWarn(t, out)
			if local {
				wantAll(t, out, "unreagent.local.yaml is laid over it: a skills.materialize there wins.")
			} else {
				wantNone(t, out, "is laid over it")
			}
		})
	}
}

// k256: before the first materialization, an unreagent-* directory in
// skills.dir that unreagent cannot show to be its own copy may be the
// project's own skill. The sweep would replace one named like a shipped skill
// and delete any other; init names each in a WARN, in -check and in init, and
// writes no skills, agents or rules file while they are there. A start keeps
// its rule (TestMaterializeRemovesDroppedSkills).
func TestInitHoldsBeforeSweepingForeignSkillDirs(t *testing.T) {
	all := shippedSkills(t)
	shipped := skillDirName(all[0].Name)
	setup := func(t *testing.T) initTree {
		tr := cleanTree(t)
		mustWriteFile(t, filepath.Join(tr.skills(), "unreagent-mine", "SKILL.md"), "---\nname: unreagent-mine\n---\nmine\n")
		mustWriteFile(t, filepath.Join(tr.skills(), shipped, "SKILL.md"), "---\nname: "+shipped+"\n---\nmy own version\n")
		mustWriteFile(t, filepath.Join(tr.skills(), "own-skill", "SKILL.md"), "mine, not prefixed\n")
		return tr
	}
	warn := "[WARN]     skills.dir "
	t.Run("check", func(t *testing.T) {
		tr := setup(t)
		before := snapshot(t, tr.dir, true)
		out, _ := tr.run(true)
		sameSnapshot(t, "init -check", before, snapshot(t, tr.dir, true))
		wantAll(t, out, warn, shipped+" (replaced)", "unreagent-mine (deleted)", "`unreagent init` would leave them",
			"but the unreagent.yaml it writes at "+tr.config()+" turns skills.materialize on, so the first start after it replaces those named like a shipped skill and deletes the others, unless you move them first")
		wantNone(t, out, "own-skill", "as it always does")
	})
	t.Run("init", func(t *testing.T) {
		tr := setup(t)
		skillsBefore := snapshot(t, tr.skills(), true)
		out, err := tr.run(false)
		if err != nil {
			t.Fatalf("init failed on warnings alone: %v\n%s", err, out)
		}
		sameSnapshot(t, "skills.dir", skillsBefore, snapshot(t, tr.skills(), true))
		if fileExists(filepath.Join(tr.claude(), "agents")) || fileExists(rulesPath(tr.claude())) {
			t.Error("init wrote agents or the rules file while holding")
		}
		wantAll(t, out, warn+tr.skills()+" holds unreagent-* directories unreagent cannot tell are its own: "+shipped+" (replaced), unreagent-mine (deleted)",
			"init left them and wrote no skills, topic agents or rules file",
			// init has just written the config that turns materialize on: the
			// next start sweeps, and the WARN says so plainly.
			"but skills.materialize is on in the unreagent.yaml init has just written ("+tr.config()+"), so the next start of the launcher replaces those named like a shipped skill and deletes the others, unless you move them first",
			"Fix: move your own out of "+tr.skills(),
			"before the next start",
			"If they are only copies an earlier unreagent wrote, there is nothing to keep: start the launcher",
			"agents: init writes none while the directories above are there; a start writes ",
			"0 blocking, 1 warning(s)")
		wantNone(t, out, "Wrote the shipped skills", "as it always does")
		// The config and the starter are written regardless.
		if !fileExists(tr.config()) || !fileExists(tr.claudeMD()) {
			t.Error("init wrote no config or no CLAUDE.md")
		}

		// Moved out of the way: the next init sets the project up.
		if err := os.RemoveAll(filepath.Join(tr.skills(), "unreagent-mine")); err != nil {
			t.Fatal(err)
		}
		if err := os.RemoveAll(filepath.Join(tr.skills(), shipped)); err != nil {
			t.Fatal(err)
		}
		out, err = tr.run(false)
		if err != nil {
			t.Fatalf("init failed: %v\n%s", err, out)
		}
		wantSetUp(t, tr)
		wantNoSetupWarn(t, out)
		if !fileExists(filepath.Join(tr.skills(), "own-skill", "SKILL.md")) {
			t.Error("init removed a skill without the prefix")
		}
	})
	// An existing config that has materialize on names itself as the file.
	t.Run("existing config", func(t *testing.T) {
		tr := setup(t)
		mustWriteFile(t, tr.config(), "agent: {enabled: true}\nmcp: {enabled: true}\nskills: {materialize: true}\n")
		out, _ := tr.run(false)
		wantAll(t, out, "but skills.materialize is on in "+tr.config()+", so the next start of the launcher replaces")
	})
	// One that unreagent.local.yaml turns on: that file decides.
	t.Run("local overlay", func(t *testing.T) {
		tr := setup(t)
		mustWriteFile(t, tr.config(), "agent: {enabled: true}\nmcp: {enabled: true}\n")
		local := filepath.Join(tr.dir, "unreagent.local.yaml")
		mustWriteFile(t, local, "skills: {materialize: true}\n")
		out, _ := tr.run(false)
		wantAll(t, out, "but skills.materialize is on in "+local+", so the next start of the launcher replaces")
	})
	t.Run("materialize off", func(t *testing.T) {
		tr := setup(t)
		mustWriteFile(t, tr.config(), "agent: {enabled: true}\nmcp: {enabled: true}\n")
		out, _ := tr.run(false)
		wantAll(t, out, warn, shipped+" (replaced)", "unreagent-mine (deleted)", "once skills.materialize is on, the first start replaces")
	})
	// A symlink or a file with the prefix is never touched by the sweep, so
	// it is not named either; a work directory an interrupted start left is
	// the launcher's by its name (skillWorkPattern).
	t.Run("link, file and work directory", func(t *testing.T) {
		tr := cleanTree(t)
		mustWriteFile(t, filepath.Join(tr.skills(), "unreagent-file"), "x")
		mustWriteFile(t, filepath.Join(tr.skills(), "unreagent-.tmp-123", "previous", "SKILL.md"), "x")
		target := t.TempDir()
		if err := os.Symlink(target, filepath.Join(tr.skills(), "unreagent-link")); err != nil {
			t.Skipf("cannot create a symlink here: %v", err)
		}
		out, _ := tr.run(true)
		wantNone(t, out, "unreagent-file", "unreagent-link", "unreagent-.tmp-")
		wantNoSetupWarn(t, out)
	})
}

// Once the launcher has materialized into skills.dir — an agent or rules
// file of its own next to it shows that — its sweep has run there, and init
// refreshes like a start, sweeping included; so does a directory that holds
// exactly today's copy of a skill, where there is no agent file to show it
// (skills.agents false).
func TestInitAfterAMaterializationRefreshesLikeAStart(t *testing.T) {
	// materializedOnce is a project an init has set up, whose shipped skill
	// directories then differ from today's copy, as after an unreagent update:
	// one with a file of the user's in it, one with other content.
	materializedOnce := func(t *testing.T) (initTree, string) {
		tr := cleanTree(t)
		if out, err := tr.run(false); err != nil {
			t.Fatalf("init failed: %v\n%s", err, out)
		}
		all := shippedSkills(t)
		shipped := skillDirName(all[0].Name)
		mustWriteFile(t, filepath.Join(tr.skills(), shipped, "my-notes.md"), "mine\n")
		mustWriteFile(t, filepath.Join(tr.skills(), skillDirName(all[1].Name), skillFile), "an earlier unreagent's version\n")
		return tr, shipped
	}

	// k256: with the marker, a directory named like a shipped skill is the
	// launcher's from an earlier version; refreshing it is what every start
	// does, and neither -check nor init warns about it.
	t.Run("shipped names only", func(t *testing.T) {
		tr, shipped := materializedOnce(t)
		before := snapshot(t, tr.dir, true)
		out, err := tr.run(true)
		if err != nil {
			t.Fatalf("init -check failed: %v\n%s", err, out)
		}
		sameSnapshot(t, "init -check", before, snapshot(t, tr.dir, true))
		wantNoSetupWarn(t, out)
		wantNone(t, out, shipped+" (replaced)", "not named like a shipped skill")
		wantAll(t, out, "0 blocking, 0 warning(s)")

		out, err = tr.run(false)
		if err != nil {
			t.Fatalf("init failed: %v\n%s", err, out)
		}
		wantNoSetupWarn(t, out)
		wantNone(t, out, shipped+" (replaced)", "not named like a shipped skill")
		wantAll(t, out, "0 blocking, 0 warning(s)")
		wantSetUp(t, tr)
		if fileExists(filepath.Join(tr.skills(), shipped, "my-notes.md")) {
			t.Error("init did not refresh the shipped skill's directory")
		}
	})

	// One that is not named like a shipped skill — the user's own, or a skill
	// an unreagent update dropped — is deleted by the sweep: named in -check,
	// and in init before it goes; the shipped ones are not.
	t.Run("own directory", func(t *testing.T) {
		tr, shipped := materializedOnce(t)
		mine := filepath.Join(tr.skills(), "unreagent-mine")
		mustWriteFile(t, filepath.Join(mine, "SKILL.md"), "mine\n")

		before := snapshot(t, tr.dir, true)
		out, err := tr.run(true)
		if err != nil {
			t.Fatalf("init -check failed: %v\n%s", err, out)
		}
		sameSnapshot(t, "init -check", before, snapshot(t, tr.dir, true))
		wantAll(t, out, "[WARN]     skills.dir "+tr.skills()+" holds unreagent-* directories not named like a shipped skill: unreagent-mine (deleted) — "+
			"an agent or rules file the launcher wrote next to it shows an earlier materialization there, so `unreagent init` and every start delete them",
			"Fix: move them out of "+tr.skills()+", or rename them without the unreagent- prefix, before you run `unreagent init` or start the launcher",
			"0 blocking, 1 warning(s)")
		wantNone(t, out, shipped, "(replaced)")

		// The name is in init's output before the directory goes.
		w := &existsWhenNamed{name: "unreagent-mine", path: mine}
		err = initRun(func() (*config.Config, config.Info, error) { return config.LoadForInit("", tr.dir) }, false, w)
		out = w.String()
		if err != nil {
			t.Fatalf("init failed: %v\n%s", err, out)
		}
		if !w.named || !w.existed {
			t.Errorf("init did not name unreagent-mine before deleting it (named %v, existed then %v):\n%s", w.named, w.existed, out)
		}
		wantAll(t, out, "[WARN]     skills.dir "+tr.skills()+" held unreagent-* directories not named like a shipped skill: unreagent-mine (deleted) — "+
			"an agent or rules file the launcher wrote next to it shows an earlier materialization there, so init deleted them, as every start does",
			"Fix: if one of them was your own, get it back from version control or a backup, and keep your own skills under a name without the unreagent- prefix",
			"0 blocking, 1 warning(s)")
		wantNone(t, out, shipped, "(replaced)")
		if fileExists(mine) {
			t.Error("init after a materialization did not sweep as a start does")
		}
		wantSetUp(t, tr)
	})

	// Materialize turned off since: the marker still makes the shipped names
	// the launcher's, so only the others are named, as deleted once it is on.
	t.Run("materialize off since", func(t *testing.T) {
		tr, shipped := materializedOnce(t)
		mustWriteFile(t, filepath.Join(tr.skills(), "unreagent-mine", "SKILL.md"), "mine\n")
		mustWriteFile(t, tr.config(), "agent: {enabled: true}\nmcp: {enabled: true}\n")
		out, _ := tr.run(true)
		wantAll(t, out, "[WARN]     skills.dir "+tr.skills()+" holds unreagent-* directories not named like a shipped skill: unreagent-mine (deleted) — once skills.materialize is on, the first start deletes them")
		wantNone(t, out, shipped, "(replaced)")
	})
	t.Run("same copy, no agents", func(t *testing.T) {
		tr := cleanTree(t)
		mustWriteFile(t, tr.config(), "agent: {enabled: true}\nmcp: {enabled: true}\nskills: {materialize: true, agents: false}\n")
		if out, err := tr.run(false); err != nil {
			t.Fatalf("init failed: %v\n%s", err, out)
		}
		out, err := tr.run(false)
		if err != nil {
			t.Fatalf("second init failed: %v\n%s", err, out)
		}
		wantNoSetupWarn(t, out)
		wantAll(t, out, "materialized into "+tr.skills())
	})
}

// Where the config makes part of this not apply, the findings say so: skills
// off, skills.dir not under .claude, skills.agents false; and an agent file in
// the way is named, in -check and in init, and left as it is.
func TestInitSetupFindingsSayWhatApplies(t *testing.T) {
	for name, c := range map[string]struct {
		config string
		wants  []string
	}{
		"skills off":     {"agent: {enabled: true}\nmcp: {enabled: true}\nskills: {enabled: false}\n", []string{"[info]     skills: off (skills.enabled is false)"}},
		"not in .claude": {"agent: {enabled: true}\nmcp: {enabled: true}\nskills: {materialize: true, dir: \"${PROJECT_DIR}/ue-skills\"}\n", []string{"agents: not written", "is not <project>/.claude/skills"}},
		"agents off":     {"agent: {enabled: true}\nmcp: {enabled: true}\nskills: {materialize: true, agents: false}\n", []string{"skills.agents is false"}},
	} {
		t.Run(name, func(t *testing.T) {
			tr := cleanTree(t)
			mustWriteFile(t, tr.config(), c.config)
			for _, check := range []bool{true, false} {
				out, err := tr.run(check)
				if err != nil {
					t.Fatalf("init (check %v) failed: %v\n%s", check, err, out)
				}
				wantAll(t, out, c.wants...)
				wantNoSetupWarn(t, out)
			}
			if fileExists(filepath.Join(tr.claude(), "agents")) || fileExists(rulesPath(tr.claude())) {
				t.Error("agents or the rules file written where they do not apply")
			}
		})
	}

	t.Run("agent file in the way", func(t *testing.T) {
		tr := cleanTree(t)
		set := shippedAgentSet(t)
		mine := agentPath(tr.claude(), set.Agents[0].Name)
		mustWriteFile(t, mine, "my own\n")
		out, _ := tr.run(true)
		wantAll(t, out, "[WARN]     agents: in the way and left alone, not written by unreagent or edited since: "+mine)
		out, _ = tr.run(false)
		wantAll(t, out, "[WARN]     agent files left alone", mine)
		if got := mustReadFile(t, mine); got != "my own\n" {
			t.Errorf("init overwrote %s", mine)
		}
	})
}

// -check writes nothing in any of these setups: no config, no CLAUDE.md, no
// skills, no agents, no rules file, and nothing rewritten in place. Neither
// does -config, which names a config to look at (k246, k256), here one that
// materializes, next to a project without CLAUDE.md.
func TestInitCheckWritesNothingAnywhere(t *testing.T) {
	t.Run("-config with materialize", func(t *testing.T) {
		tr := cleanTree(t)
		custom := filepath.Join(tr.dir, "custom.yaml")
		mustWriteFile(t, custom, "agent: {enabled: true}\nmcp: {enabled: true}\nskills: {materialize: true}\n")
		before := snapshot(t, tr.dir, true)
		var out bytes.Buffer
		if err := initCommand([]string{"-config", custom}, &out); err != nil {
			t.Fatalf("init -config failed: %v\n%s", err, out.String())
		}
		sameSnapshot(t, "init -config", before, snapshot(t, tr.dir, true))
		wantNone(t, out.String(), "Wrote ")
	})
	for name, prepare := range map[string]func(t *testing.T, tr initTree){
		"fresh":         func(t *testing.T, tr initTree) {},
		"own CLAUDE.md": func(t *testing.T, tr initTree) { mustWriteFile(t, tr.claudeMD(), "# mine\n") },
		"config without materialize": func(t *testing.T, tr initTree) {
			mustWriteFile(t, tr.config(), "agent: {enabled: true}\nmcp: {enabled: true}\n")
		},
		"config with materialize": func(t *testing.T, tr initTree) {
			mustWriteFile(t, tr.config(), "agent: {enabled: true}\nmcp: {enabled: true}\nskills: {materialize: true}\n")
		},
		"foreign skill dir": func(t *testing.T, tr initTree) {
			mustWriteFile(t, filepath.Join(tr.skills(), "unreagent-mine", "SKILL.md"), "mine\n")
		},
		"set up, then a stale agent": func(t *testing.T, tr initTree) {
			if out, err := tr.run(false); err != nil {
				t.Fatalf("init failed: %v\n%s", err, out)
			}
			// Ours, but not today's: -check must not refresh it.
			set := shippedAgentSet(t)
			a := set.Agents[0]
			a.Body += "\nolder text\n"
			mustWriteFile(t, agentPath(tr.claude(), a.Name), renderAgent(a))
		},
	} {
		t.Run(name, func(t *testing.T) {
			tr := cleanTree(t)
			prepare(t, tr)
			before := snapshot(t, tr.dir, true)
			out, err := tr.run(true)
			if err != nil {
				t.Fatalf("init -check failed: %v\n%s", err, out)
			}
			sameSnapshot(t, "init -check", before, snapshot(t, tr.dir, true))
			wantNone(t, out, "Wrote ")
		})
	}
}

// k256 review: an unreagent.local.yaml that turns materialize off over an
// unreagent.yaml that has it on is the file to fix, and the finding names it,
// not the lines unreagent.yaml holds already. Where neither has it on, both
// files are named.
func TestInitMaterializeOffNamesTheDecidingFile(t *testing.T) {
	local := func(tr initTree) string { return filepath.Join(tr.dir, "unreagent.local.yaml") }
	off := "skills: {materialize: false}\n"
	t.Run("fresh project", func(t *testing.T) {
		for _, check := range []bool{true, false} {
			tr := cleanTree(t)
			mustWriteFile(t, local(tr), off)
			out, err := tr.run(check)
			if err != nil {
				t.Fatalf("init (check %v) failed: %v\n%s", check, err, out)
			}
			base := "unreagent.yaml" // the one init has just written
			if check {
				base = "the unreagent.yaml init writes"
			}
			wantAll(t, out, "[info]     skills: not written into the project — skills.materialize is off",
				"unreagent.local.yaml sets skills.materialize: false and is laid over "+base+", which turns it on: remove skills.materialize from unreagent.local.yaml to have every start write them.")
			wantNone(t, out, "add to unreagent.yaml")
		}
	})
	t.Run("existing config with materialize on", func(t *testing.T) {
		tr := cleanTree(t)
		mustWriteFile(t, tr.config(), "agent: {enabled: true}\nmcp: {enabled: true}\nskills: {materialize: true}\n")
		mustWriteFile(t, local(tr), off)
		out, _ := tr.run(false)
		wantAll(t, out, "unreagent.local.yaml sets skills.materialize: false and is laid over unreagent.yaml, which turns it on: remove skills.materialize from unreagent.local.yaml")
		wantNone(t, out, "add to unreagent.yaml")
	})
	t.Run("off in both", func(t *testing.T) {
		tr := cleanTree(t)
		mustWriteFile(t, tr.config(), "agent: {enabled: true}\nmcp: {enabled: true}\n")
		mustWriteFile(t, local(tr), off)
		out, _ := tr.run(false)
		wantAll(t, out, "remove skills.materialize from unreagent.local.yaml, which is laid over unreagent.yaml and wins, and add to unreagent.yaml (into its skills: section, if it has one):\n")
		wantNone(t, out, "which turns it on")
	})
	// k256 review round 2: adding a second materialize key to a skills:
	// section that already has one breaks the config, so a false the config
	// sets is changed, not added to.
	t.Run("off in unreagent.yaml", func(t *testing.T) {
		tr := cleanTree(t)
		mustWriteFile(t, tr.config(), "agent: {enabled: true}\nmcp: {enabled: true}\nskills:\n  materialize: false\n")
		out, _ := tr.run(true)
		wantAll(t, out, "Fix: to have every start write them, change skills.materialize from false to true in unreagent.yaml.")
		wantNone(t, out, "add to unreagent.yaml")
	})
	// Every combination of the two files, each absent, without the key, with
	// it true or false: where materialize ends up off, the fix is followed
	// word for word and init run again, which must then load a config with
	// materialize on and write the skills.
	t.Run("every combination, fix applied", func(t *testing.T) {
		files := map[string]string{
			"absent": "",
			"no key": "agent: {enabled: true}\nmcp: {enabled: true}\nskills:\n  agents: true\n",
			"true":   "agent: {enabled: true}\nmcp: {enabled: true}\nskills:\n  materialize: true\n",
			"false":  "agent: {enabled: true}\nmcp: {enabled: true}\nskills:\n  materialize: false\n",
		}
		locals := map[string]string{
			"absent": "",
			"no key": "mcp: {enabled: true}\n",
			"true":   "skills:\n  materialize: true\n",
			"false":  "skills:\n  materialize: false\n",
		}
		const name = `unreagent(?:\.local)?\.yaml`
		remove := regexp.MustCompile(`remove skills\.materialize from (` + name + `)`)
		change := regexp.MustCompile(`change skills\.materialize from false to true in (` + name + `)`)
		add := regexp.MustCompile(`add to (` + name + `) \(into its skills: section, if it has one\):\n +skills:\n +materialize: true\n`)
		for bk, base := range files {
			for lk, loc := range locals {
				t.Run("unreagent.yaml "+bk+", local "+lk, func(t *testing.T) {
					tr := cleanTree(t)
					path := map[string]string{"unreagent.yaml": tr.config(), "unreagent.local.yaml": local(tr)}
					if base != "" {
						mustWriteFile(t, tr.config(), base)
					}
					if loc != "" {
						mustWriteFile(t, local(tr), loc)
					}
					wantOff := lk == "false" || (lk != "true" && (bk == "no key" || bk == "false"))
					out, err := tr.run(true)
					if err != nil {
						t.Fatalf("init -check failed: %v\n%s", err, out)
					}
					if !wantOff {
						wantNone(t, out, "skills.materialize is off")
						return
					}
					wantAll(t, out, "skills.materialize is off")
					applied := 0
					for _, m := range remove.FindAllStringSubmatch(out, -1) {
						p := path[m[1]]
						kept := regexp.MustCompile(`(?m)^ *materialize:.*\n`).ReplaceAllString(mustReadFile(t, p), "")
						mustWriteFile(t, p, kept)
						applied++
					}
					for _, m := range change.FindAllStringSubmatch(out, -1) {
						p := path[m[1]]
						mustWriteFile(t, p, strings.Replace(mustReadFile(t, p), "materialize: false", "materialize: true", 1))
						applied++
					}
					for _, m := range add.FindAllStringSubmatch(out, -1) {
						p := path[m[1]]
						doc := mustReadFile(t, p)
						if strings.Contains(doc, "skills:\n") {
							doc = strings.Replace(doc, "skills:\n", "skills:\n  materialize: true\n", 1)
						} else {
							doc += "skills:\n  materialize: true\n"
						}
						mustWriteFile(t, p, doc)
						applied++
					}
					if applied == 0 {
						t.Fatalf("no fix found to apply:\n%s", out)
					}
					out, err = tr.run(false) // "Then run `unreagent init` again."
					if err != nil {
						t.Fatalf("init after the fix failed: %v\n%s", err, out)
					}
					wantNone(t, out, "skills.materialize is off")
					cfg, _, err := config.LoadForInit("", tr.dir)
					if err != nil {
						t.Fatalf("config after the fix does not load: %v", err)
					}
					if !cfg.Skills.ViaFiles() {
						t.Errorf("config after the fix has materialize off:\nunreagent.yaml:\n%s\nunreagent.local.yaml:\n%s", mustReadFile(t, tr.config()), readOrNone(local(tr)))
					}
					wantSetUp(t, tr)
				})
			}
		}
	})
}

// readOrNone is the file's content, or "(none)" when it cannot be read.
func readOrNone(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return "(none)"
	}
	return string(b)
}

// k256 review: holdsCopyOf proves a directory the launcher's own only when it
// holds exactly the files materialization writes, each a regular file. A
// file the user added to an otherwise exact copy, one of its files missing,
// or a symlink in place of one with the same content makes it unproven, and
// init names it (here before any materialization, so it holds).
func TestInitNamesAnAlmostExactCopy(t *testing.T) {
	all := shippedSkills(t)
	t.Run("extra file", func(t *testing.T) {
		tr := cleanTree(t)
		if err := materializeSkills(tr.skills(), all); err != nil {
			t.Fatal(err)
		}
		shipped := skillDirName(all[0].Name)
		mustWriteFile(t, filepath.Join(tr.skills(), shipped, "my-notes.md"), "mine\n")
		out, _ := tr.run(true)
		wantAll(t, out, "holds unreagent-* directories unreagent cannot tell are its own: "+shipped+" (replaced). ")
	})
	// A copy that misses one of the shipped files is not the launcher's
	// either: the sweep would replace it, so it is named.
	t.Run("missing file", func(t *testing.T) {
		dir := t.TempDir()
		if err := materializeSkills(dir, all); err != nil {
			t.Fatal(err)
		}
		var with skills.Skill
		for _, sk := range all {
			if len(sk.Files) > 0 {
				with = sk
				break
			}
		}
		if with.Name == "" {
			t.Skip("no shipped skill has a section file")
		}
		shipped := skillDirName(with.Name)
		for section := range with.Files {
			if err := os.Remove(filepath.Join(dir, shipped, section+".md")); err != nil {
				t.Fatal(err)
			}
			break
		}
		replaced, removed, err := unprovenSkillDirs(dir, all)
		if err != nil || len(removed) != 0 || strings.Join(replaced, ",") != shipped {
			t.Errorf("unprovenSkillDirs = %v, %v, %v; want only %s replaced", replaced, removed, err, shipped)
		}
	})
	t.Run("symlinked file", func(t *testing.T) {
		dir := t.TempDir()
		if err := materializeSkills(dir, all); err != nil {
			t.Fatal(err)
		}
		shipped := skillDirName(all[0].Name)
		skillMD := filepath.Join(dir, shipped, "SKILL.md")
		elsewhere := filepath.Join(t.TempDir(), "SKILL.md")
		mustWriteFile(t, elsewhere, mustReadFile(t, skillMD))
		if err := os.Remove(skillMD); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(elsewhere, skillMD); err != nil {
			t.Skipf("cannot create a symlink here: %v", err)
		}
		replaced, removed, err := unprovenSkillDirs(dir, all)
		if err != nil || len(removed) != 0 || strings.Join(replaced, ",") != shipped {
			t.Errorf("unprovenSkillDirs = %v, %v, %v; want only %s replaced", replaced, removed, err, shipped)
		}
	})
}

// k256 review: init's closing line names what this run wrote — the config,
// CLAUDE.md, the skills, the agent and rules files — and says nothing only
// when it wrote nothing. Without an engine every run ends on it.
func TestInitClosingLineNamesWhatItWrote(t *testing.T) {
	all, set := shippedSkills(t), shippedAgentSet(t)
	closing := func(t *testing.T, tr initTree, check bool) string {
		t.Helper()
		out, err := tr.run(check)
		if strings.Contains(out, "[ok]       engine: ") {
			t.Skip("this host has an engine config.Load detects without UE_ROOT")
		}
		if err == nil {
			t.Fatalf("init without an engine succeeded:\n%s", out)
		}
		_, after, ok := strings.Cut(err.Error(), "(unreagent init wrote ")
		if !ok {
			t.Fatalf("err = %v, want it to end on what init wrote", err)
		}
		return strings.TrimSuffix(after, ")")
	}
	skillsPart := func(tr initTree) string {
		return fmt.Sprintf("the %d shipped skills into %s", len(all), tr.skills())
	}
	t.Run("fresh, then again", func(t *testing.T) {
		tr := newInitTree(t, "", "node", "npm", "claude")
		want := tr.config() + ", " + tr.claudeMD() + ", " + skillsPart(tr) + " and " +
			fmt.Sprintf("%d files of the topic agents and the rules file into %s", len(set.Agents)+1, tr.claude())
		if got := closing(t, tr, false); got != want {
			t.Errorf("fresh: init wrote %q, want %q", got, want)
		}
		// Config, CLAUDE.md and agents are there; the skills are written anew
		// on every run, as at every start.
		if got := closing(t, tr, false); got != skillsPart(tr) {
			t.Errorf("again: init wrote %q, want %q", got, skillsPart(tr))
		}
		if got := closing(t, tr, true); got != "nothing" {
			t.Errorf("-check: init wrote %q, want nothing", got)
		}
	})
	// k256 review round 2: when some skills could not be written, the line
	// init prints at the time says "part of" as the closing line does.
	t.Run("skills partly written", func(t *testing.T) {
		if runtime.GOOS == "windows" || os.Geteuid() == 0 {
			t.Skip("needs a directory the test user cannot write to")
		}
		tr := newInitTree(t, "", "node", "npm", "claude")
		mustWriteFile(t, tr.config(), "agent: {enabled: true, claudeIntegration: true}\nskills: {materialize: true}\n")
		if err := os.MkdirAll(tr.skills(), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(tr.skills(), 0o555); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(tr.skills(), 0o755) })
		out, err := tr.run(false)
		if err == nil || !strings.Contains(err.Error(), "part of the shipped skills into "+tr.skills()) {
			t.Fatalf("err = %v, want the closing line to say part of the skills\n%s", err, out)
		}
		wantAll(t, out, "Wrote part of the shipped skills into "+tr.skills()+" ")
		wantNone(t, out, "Wrote the shipped skills")
	})
	t.Run("config without materialize", func(t *testing.T) {
		tr := newInitTree(t, "", "node", "npm", "claude")
		mustWriteFile(t, tr.config(), "unreal: {editor: claude}\n")
		if got := closing(t, tr, false); got != tr.claudeMD() {
			t.Errorf("init wrote %q, want %q", got, tr.claudeMD())
		}
		if got := closing(t, tr, false); got != "nothing" {
			t.Errorf("again: init wrote %q, want nothing", got)
		}
	})
}

// k256 review: two cases the files init writes do not reach, said in a
// finding and nothing else: an agent.workdir away from the project directory
// (Claude Code reads CLAUDE.md from its working directory upward), and an
// agent that is not Claude Code, which reads none of CLAUDE.md, the topic
// agents and the rules file.
func TestInitSaysWhatTheAgentDoesNotRead(t *testing.T) {
	const base = "mcp: {enabled: true}\nskills: {materialize: true}\n"
	elsewhere := "CLAUDE.md, the topic agents and the rules file are read by Claude Code only"
	t.Run("workdir elsewhere", func(t *testing.T) {
		tr := cleanTree(t)
		work := filepath.Join(t.TempDir(), "work")
		mustWriteFile(t, tr.config(), base+"agent: {enabled: true, workdir: \""+filepath.ToSlash(work)+"\"}\n")
		for _, check := range []bool{true, false} {
			out, _ := tr.run(check)
			wantAll(t, out, "CLAUDE.md was looked for in the project directory "+tr.dir+" (CLAUDE.md and .claude/CLAUDE.md)",
				"agent.workdir is "+filepath.ToSlash(work)+", so the agent's working directory is elsewhere, and Claude Code reads CLAUDE.md from its working directory and the directories above it")
		}
	})
	t.Run("workdir is the project", func(t *testing.T) {
		tr := cleanTree(t)
		mustWriteFile(t, tr.config(), base+"agent: {enabled: true, claudeIntegration: true, workdir: \""+filepath.ToSlash(tr.dir)+"\"}\n")
		out, _ := tr.run(false)
		wantNone(t, out, "agent.workdir is", elsewhere)
	})
	// k256 review round 2: below the project directory, Claude Code finds the
	// project's CLAUDE.md on its way up, so there is nothing to say. A sibling
	// whose name only starts like the project's is not below it.
	t.Run("workdir below the project", func(t *testing.T) {
		tr := cleanTree(t)
		work := filepath.Join(tr.dir, "Saved", "agent")
		mustWriteFile(t, tr.config(), base+"agent: {enabled: true, claudeIntegration: true, workdir: \""+filepath.ToSlash(work)+"\"}\n")
		out, _ := tr.run(true)
		wantNone(t, out, "agent.workdir is", elsewhere)
	})
	t.Run("workdir next to the project, same name start", func(t *testing.T) {
		tr := cleanTree(t)
		work := tr.dir + "-work"
		mustWriteFile(t, tr.config(), base+"agent: {enabled: true, claudeIntegration: true, workdir: \""+filepath.ToSlash(work)+"\"}\n")
		out, _ := tr.run(true)
		wantAll(t, out, "agent.workdir is "+filepath.ToSlash(work)+", so the agent's working directory is elsewhere")
	})
	for name, agent := range map[string]string{
		"claudeIntegration off": "agent: {enabled: true, claudeIntegration: false}\n",
		"hermes":                "agent: {enabled: true, claudeIntegration: true, hermes: {enabled: true}}\n",
	} {
		t.Run(name, func(t *testing.T) {
			tr := cleanTree(t)
			mustWriteFile(t, tr.config(), base+agent)
			out, _ := tr.run(true)
			wantAll(t, out, "[info]     agent files: "+elsewhere+" — agent."+map[bool]string{true: "hermes.enabled is set", false: "claudeIntegration is false"}[name == "hermes"])
		})
	}
	t.Run("claude code", func(t *testing.T) {
		tr := cleanTree(t)
		out, _ := tr.run(true)
		wantNone(t, out, elsewhere)
	})
}

// existsWhenNamed is init's output, which records whether path still existed
// the first time a write named name.
type existsWhenNamed struct {
	bytes.Buffer
	name, path     string
	named, existed bool
}

func (w *existsWhenNamed) Write(p []byte) (int, error) {
	if !w.named && bytes.Contains(p, []byte(w.name)) {
		w.named, w.existed = true, fileExists(w.path)
	}
	return w.Buffer.Write(p)
}
