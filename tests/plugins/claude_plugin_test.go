package plugins_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// -----------------------------------------------------------------------------
// [Test Level]: Integration Test
// [Purpose]: Verifies the parts of the plugin that only Claude Code reads: its MCP server override and the prompt hook.
// [Execution Surface]: Filesystem Canonical Plugin Bundle (packaging/plugins/gyrus/)
// [Assertions]: .claude-plugin/plugin.json redeclares the gyrus server with GYRUS_PROJECT=${CLAUDE_PROJECT_DIR}
// (Claude Code leaves ${workspaceFolder} literal); hooks/hooks.json registers a UserPromptSubmit command running
// 'gyrus suggest-context --hook' with a token budget and a timeout.
// -----------------------------------------------------------------------------
func TestClaudePluginOverrideAndPromptHook(t *testing.T) {
	pluginDir := filepath.Join("..", "..", "packaging", "plugins", "gyrus")

	var manifest struct {
		MCPServers map[string]struct {
			Command string            `json:"command"`
			Env     map[string]string `json:"env"`
		} `json:"mcpServers"`
	}
	data, err := os.ReadFile(filepath.Join(pluginDir, ".claude-plugin", "plugin.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("invalid Claude plugin.json: %v", err)
	}
	server, ok := manifest.MCPServers["gyrus"]
	if !ok || server.Command != "gyrus" || server.Env["GYRUS_PROJECT"] != "${CLAUDE_PROJECT_DIR}" {
		t.Errorf("Claude plugin.json must redeclare the gyrus server with ${CLAUDE_PROJECT_DIR}, got %+v", manifest.MCPServers)
	}

	var hooks struct {
		Hooks map[string][]struct {
			Hooks []struct {
				Type    string `json:"type"`
				Command string `json:"command"`
				Timeout int    `json:"timeout"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	data, err = os.ReadFile(filepath.Join(pluginDir, "hooks", "hooks.json"))
	if err != nil {
		t.Fatalf("missing hooks/hooks.json: %v", err)
	}
	if err := json.Unmarshal(data, &hooks); err != nil {
		t.Fatalf("invalid hooks.json: %v", err)
	}
	entries := hooks.Hooks["UserPromptSubmit"]
	if len(entries) != 1 || len(entries[0].Hooks) != 1 {
		t.Fatalf("expected one UserPromptSubmit hook, got %+v", hooks.Hooks)
	}
	h := entries[0].Hooks[0]
	if h.Type != "command" || !strings.HasPrefix(h.Command, "gyrus suggest-context --hook") || !strings.Contains(h.Command, "--max-tokens") || h.Timeout <= 0 {
		t.Errorf("unexpected prompt hook: %+v", h)
	}
}
