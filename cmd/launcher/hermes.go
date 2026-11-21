package main

import (
	"bytes"
	cryptorand "crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/conflict-industries/unreagent/internal/config"

	"gopkg.in/yaml.v3"
)

// hermesConfigMarker is the comment that tags the mcp_servers block we write.
// We prepend it to the file as a YAML comment so the user knows: this block
// was written by unreagent.
const hermesConfigMarker = "# unreagent-managed: do not edit — re-run `unreagent hermes-setup` to refresh"

// runHermesSetup is the entry point for `unreagent hermes-setup`. It merges
// the unreagent MCP server entry into Hermes Agent's config.yaml so Hermes
// (Nous Research) can use the same toolset as Claude Code.
//
// Idempotent: a second run overwrites the previous entry instead of duplicating
// it. The unreagent config is **not** auto-merged on normal launcher runs —
// the user has to opt in by running this command once.
func runHermesSetup() error {
	// Use a private FlagSet so the sub-command is self-contained (the main
	// flags are parsed in run(), which we never reach here). os.Args[0] is the
	// binary path. To make `-config` work in either order, we splice ONLY the
	// sub-command name out of os.Args[1:] and hand the rest to fs.Parse. That
	// way the FlagSet sees exactly the flags the user typed — flag.Parse does
	// stop at the first non-flag arg, but since the sub-command word is gone,
	// the next value (if any) is already a flag-pair.
	_, subIndex := extractSubcommand()
	filtered := make([]string, 0, len(os.Args))
	filtered = append(filtered, os.Args[:subIndex]...)   // binary + everything before the sub-command
	filtered = append(filtered, os.Args[subIndex+1:]...) // everything after the sub-command
	return hermesSetup(filtered[1:], os.Stdout)
}

// hermesSetup does the work of `unreagent hermes-setup`: args are the
// sub-command's own flags, out receives the progress and summary lines.
func hermesSetup(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("hermes-setup", flag.ContinueOnError)
	cfgPath := fs.String("config", "", "Path to unreagent.yaml (default: next to the executable)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, info, err := config.Load(*cfgPath)
	if err != nil {
		return err
	}

	if !cfg.Agent.Hermes.Enabled {
		fmt.Fprintln(out, "agent.hermes.enabled is not set — nothing to do.")
		fmt.Fprintln(out, "Set it in unreagent.yaml and re-run this command to integrate Hermes.")
		fmt.Fprintln(out, "Hint: this file — "+info.ConfigPath)
		return nil
	}

	token := cfg.MCP.Token
	tokenGenerated := false
	if token == "" {
		t, err := generateBearerToken()
		if err != nil {
			return fmt.Errorf("generate token: %w", err)
		}
		token = t
		tokenGenerated = true
	}

	// Token persistence: if we just generated one, write it to
	// unreagent.local.yaml so the launcher reuses the same token on the next
	// start (otherwise Hermes' bearer header would no longer match after a
	// reload).
	if tokenGenerated {
		if err := persistGeneratedToken(info, token); err != nil {
			return fmt.Errorf("write token to %s: %w", localOverridePath(info.ConfigPath), err)
		}
		fmt.Fprintln(out, "New bearer token generated and written to unreagent.local.yaml.")
	}

	hermesPath, err := resolveHermesConfigPath(cfg.Agent.Hermes.ConfigPath)
	if err != nil {
		return err
	}
	fmt.Fprintln(out, "Hermes config: "+hermesPath)

	// Read and merge first: the merge has to start from the user's config, and
	// nothing is touched if it cannot be parsed or merged.
	doc, raw, err := loadHermesYAML(hermesPath)
	if err != nil {
		return err
	}
	if err := mergeHermesServer(doc, cfg.Agent.Hermes.ServerName, cfg.MCP.Address, token); err != nil {
		return fmt.Errorf("Hermes config %s left untouched: %w", hermesPath, err)
	}

	// Back up the existing file (the bytes just read) before it is replaced.
	// Empty file or not present — no backup needed.
	if len(raw) > 0 {
		bak, created, err := backupHermesConfig(hermesPath, raw)
		if err != nil {
			return fmt.Errorf("create backup: %w", err)
		}
		if created {
			fmt.Fprintln(out, "Backup: "+bak)
		} else {
			fmt.Fprintln(out, "Backup kept: "+bak)
		}
	}

	if err := os.MkdirAll(filepath.Dir(hermesPath), 0o755); err != nil {
		return fmt.Errorf("create target dir: %w", err)
	}
	if err := writeYAMLAtomic(hermesPath, doc); err != nil {
		return fmt.Errorf("write Hermes config: %w", err)
	}

	fmt.Fprintln(out)
	fmt.Fprintln(out, "Hermes MCP server registered under mcp_servers."+cfg.Agent.Hermes.ServerName)
	fmt.Fprintln(out, "  url:    http://"+cfg.MCP.Address+"/mcp")
	fmt.Fprintln(out, "  bearer: "+bearerSummary(token, tokenGenerated))
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Restart Hermes so it re-reads the config. The unreagent tools")
	fmt.Fprintln(out, "(ue_start/stop/restart, logs, run_command, …) will then be available.")
	return nil
}

// resolveHermesConfigPath returns the absolute path to the Hermes config:
//   - explicitly set in agent.hermes.configPath
//   - $HERMES_HOME/config.yaml, if set
//   - otherwise ~/.hermes/config.yaml
func resolveHermesConfigPath(explicit string) (string, error) {
	if explicit != "" {
		abs, err := filepath.Abs(explicit)
		if err != nil {
			return "", fmt.Errorf("make configPath absolute: %w", err)
		}
		return filepath.ToSlash(abs), nil
	}
	if h := os.Getenv("HERMES_HOME"); h != "" {
		return filepath.ToSlash(filepath.Join(h, "config.yaml")), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	return filepath.ToSlash(filepath.Join(home, ".hermes", "config.yaml")), nil
}

// generateBearerToken returns 32 random bytes as a hex string (64 chars).
// Enough entropy for a localhost bearer, small enough to stay manageable in
// YAML.
func generateBearerToken() (string, error) {
	b := make([]byte, 32)
	if _, err := cryptorand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// localOverridePath returns the path to unreagent.local.yaml next to a
// unreagent.yaml (same convention as config.localPathFor).
func localOverridePath(yamlPath string) string {
	ext := filepath.Ext(yamlPath)
	return strings.TrimSuffix(yamlPath, ext) + ".local" + ext
}

// persistGeneratedToken writes mcp.token into unreagent.local.yaml. Other
// fields in that file (e.g. engineRoot) are left alone — we only merge.
// If the file does not exist yet, it is created.
func persistGeneratedToken(info config.Info, token string) error {
	override := localOverridePath(info.ConfigPath)

	// Read any existing file so we don't clobber it.
	var existing map[string]interface{}
	if b, err := os.ReadFile(override); err == nil && len(bytes.TrimSpace(b)) > 0 {
		if uerr := yaml.Unmarshal(b, &existing); uerr != nil {
			return fmt.Errorf("existing %s not parseable: %w", filepath.Base(override), uerr)
		}
	}
	if existing == nil {
		existing = map[string]interface{}{}
	}

	mcpSection, _ := existing["mcp"].(map[string]interface{})
	if mcpSection == nil {
		mcpSection = map[string]interface{}{}
	}
	mcpSection["token"] = token
	existing["mcp"] = mcpSection

	out, err := yaml.Marshal(existing)
	if err != nil {
		return err
	}
	// YAML comment so the user knows this file was generated.
	header := "# Generated by `unreagent hermes-setup` — mcp.token was auto-\n" +
		"# generated so the unreagent MCP server accepts the bearer auth from\n" +
		"# Hermes' config. Keep the token in this (git-ignored) file.\n"
	return os.WriteFile(override, []byte(header+string(out)), 0o600)
}

// loadHermesYAML reads the Hermes config and returns a mutable map plus the
// raw bytes it was parsed from (nil if the file does not exist). A missing or
// empty file is OK (fresh Hermes install).
func loadHermesYAML(path string) (map[string]interface{}, []byte, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]interface{}{}, nil, nil
		}
		return nil, nil, fmt.Errorf("read Hermes config %s: %w", path, err)
	}
	if len(bytes.TrimSpace(b)) == 0 {
		return map[string]interface{}{}, b, nil
	}
	doc := map[string]interface{}{}
	if err := yaml.Unmarshal(b, &doc); err != nil {
		return nil, nil, fmt.Errorf("Hermes config %s not parseable: %w", path, err)
	}
	if doc == nil { // a null document ("null", "~", a bare "---") decodes to a nil map
		doc = map[string]interface{}{}
	}
	return doc, b, nil
}

// backupHermesConfig writes raw (the config as read before the merge) to
// <path>.bak. An existing .bak is never overwritten — it may hold the user's
// original from an earlier run — so created is false when one was already
// there. A failed write removes the partial backup.
func backupHermesConfig(path string, raw []byte) (bak string, created bool, err error) {
	bak = path + ".bak"
	f, err := os.OpenFile(bak, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if os.IsExist(err) {
			return bak, false, nil
		}
		return bak, false, err
	}
	if _, err := f.Write(raw); err != nil {
		f.Close()
		os.Remove(bak)
		return bak, false, err
	}
	if err := f.Close(); err != nil {
		os.Remove(bak)
		return bak, false, err
	}
	return bak, true, nil
}

// mergeHermesServer sets mcp_servers.<name> to the unreagent entry. If a
// block with that name already exists (from a previous hermes-setup run), it
// is overwritten. Other servers under mcp_servers are left alone. An
// mcp_servers that is not a mapping with string keys (a list, or a key YAML
// reads as a number) cannot be merged into without dropping its content, so
// it is refused and doc stays unchanged.
func mergeHermesServer(doc map[string]interface{}, name, address, token string) error {
	var servers map[string]interface{}
	switch v := doc["mcp_servers"].(type) {
	case nil:
		servers = map[string]interface{}{}
	case map[string]interface{}:
		servers = v
	default:
		return fmt.Errorf("mcp_servers is not a mapping of server names (a list, or a key that is not a string); refusing to replace it")
	}
	servers[name] = map[string]interface{}{
		"url": "http://" + address + "/mcp",
		"headers": map[string]interface{}{
			"Authorization": "Bearer " + token,
		},
	}
	doc["mcp_servers"] = servers
	return nil
}

// bearerSummary describes the bearer token for the setup summary. Only a
// token this command generated is known to be 64 hex chars and to live in
// unreagent.local.yaml, so only that one gets the claim and an eight-character
// prefix. A token the user set in mcp.token can have any length and is not
// echoed: a prefix of a short one is most of the secret.
func bearerSummary(token string, generated bool) string {
	if !generated {
		return fmt.Sprintf("mcp.token from your unreagent config (%d chars, not shown)", len(token))
	}
	return token[:min(8, len(token))] + "… (64 hex chars, in unreagent.local.yaml)"
}

// writeYAMLAtomic writes the config safely: first to a temp file in the same
// directory, then rename. Avoids a half-written config if the process is
// killed mid-write.
//
// We prepend a marker comment so the user knows this file is managed by
// unreagent. On a re-run the marker persists (useful as a hint that the file
// is tool-managed).
func writeYAMLAtomic(path string, doc map[string]interface{}) error {
	out, err := yaml.Marshal(doc)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // in case the rename below fails

	if _, err := tmp.WriteString(hermesConfigMarker + "\n"); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(out); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	return nil
}
