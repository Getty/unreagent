package main

import (
	"bytes"
	cryptorand "crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
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
	fs := flag.NewFlagSet("hermes-setup", flag.ContinueOnError)
	cfgPath := fs.String("config", "", "Path to unreagent.yaml (default: next to the executable)")
	if err := fs.Parse(filtered[1:]); err != nil {
		return err
	}

	cfg, info, err := config.Load(*cfgPath)
	if err != nil {
		return err
	}

	if !cfg.Agent.Hermes.Enabled {
		fmt.Println("agent.hermes.enabled is not set — nothing to do.")
		fmt.Println("Set it in unreagent.yaml and re-run this command to integrate Hermes.")
		fmt.Println("Hint: this file — " + info.ConfigPath)
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
		fmt.Println("New bearer token generated and written to unreagent.local.yaml.")
	}

	hermesPath, err := resolveHermesConfigPath(cfg.Agent.Hermes.ConfigPath)
	if err != nil {
		return err
	}
	fmt.Println("Hermes config: " + hermesPath)

	// Back up the existing file before we touch it. Empty file or not present
	// — no backup needed.
	if existing, statErr := os.Stat(hermesPath); statErr == nil && existing.Size() > 0 {
		bak := hermesPath + ".bak"
		if _, bakErr := os.Stat(bak); bakErr == nil {
			// A backup from a previous hermes-setup run is already there — do
			// not overwrite it; it is the user's value.
			fmt.Println("Backup kept: " + bak)
		} else {
			if err := os.Rename(hermesPath, bak); err != nil {
				return fmt.Errorf("create backup: %w", err)
			}
			fmt.Println("Backup: " + bak)
		}
	} else if statErr != nil && !os.IsNotExist(statErr) {
		return fmt.Errorf("stat Hermes config: %w", statErr)
	}

	doc, err := loadHermesYAML(hermesPath)
	if err != nil {
		return err
	}

	mergeHermesServer(doc, cfg.Agent.Hermes.ServerName, cfg.MCP.Address, token)

	if err := os.MkdirAll(filepath.Dir(hermesPath), 0o755); err != nil {
		return fmt.Errorf("create target dir: %w", err)
	}
	if err := writeYAMLAtomic(hermesPath, doc); err != nil {
		return fmt.Errorf("write Hermes config: %w", err)
	}

	fmt.Println()
	fmt.Println("Hermes MCP server registered under mcp_servers." + cfg.Agent.Hermes.ServerName)
	fmt.Println("  url:    http://" + cfg.MCP.Address + "/mcp")
	fmt.Println("  bearer: " + token[:8] + "… (64 hex chars, in unreagent.local.yaml)")
	fmt.Println()
	fmt.Println("Restart Hermes so it re-reads the config. The unreagent tools")
	fmt.Println("(ue_start/stop/restart, logs, run_command, …) will then be available.")
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

// loadHermesYAML reads the Hermes config and returns a mutable map. A missing
// or empty file is OK (fresh Hermes install).
func loadHermesYAML(path string) (map[string]interface{}, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]interface{}{}, nil
		}
		return nil, fmt.Errorf("read Hermes config %s: %w", path, err)
	}
	if len(bytes.TrimSpace(b)) == 0 {
		return map[string]interface{}{}, nil
	}
	doc := map[string]interface{}{}
	if err := yaml.Unmarshal(b, &doc); err != nil {
		return nil, fmt.Errorf("Hermes config %s not parseable: %w", path, err)
	}
	return doc, nil
}

// mergeHermesServer sets mcp_servers.<name> to the unreagent entry. If a
// block with that name already exists (from a previous hermes-setup run), it
// is overwritten. Other servers under mcp_servers are left alone.
func mergeHermesServer(doc map[string]interface{}, name, address, token string) {
	servers, _ := doc["mcp_servers"].(map[string]interface{})
	if servers == nil {
		servers = map[string]interface{}{}
	}
	servers[name] = map[string]interface{}{
		"url": "http://" + address + "/mcp",
		"headers": map[string]interface{}{
			"Authorization": "Bearer " + token,
		},
	}
	doc["mcp_servers"] = servers
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
