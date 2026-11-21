package skills

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
)

// Paths inside a UE LLM Toolkit checkout.
const (
	toolkitMCP      = "Plugin/UELLMToolkit/Source/UELLMToolkit/Private/MCP"
	toolkitRegistry = toolkitMCP + "/MCPToolRegistry.cpp"
	toolkitBridge   = "Plugin/UELLMToolkit/Resources/mcp-bridge/index.js"
)

var (
	// registerRe matches one registration in MCPToolRegistry.cpp:
	// RegisterTool(MakeShared<FMCPTool_X>()) or (MakeShared<FMCPTool_X>(TaskQueue)).
	registerRe = regexp.MustCompile(`\bRegisterTool\s*\(\s*MakeShared\s*<\s*(FMCPTool_\w+)\s*>`)
	// toolNameRe matches the name a tool declares in its GetInfo.
	toolNameRe = regexp.MustCompile(`\bInfo\.Name\s*=\s*TEXT\s*\(\s*"([^"\\]*)"\s*\)`)
	// bridgeRe matches a tool the mcp-bridge defines itself, such as
	// name: "unreal_get_ue_context". The registry's tools are listed there
	// through a template (`unreal_${tool.name}`), which this does not match.
	bridgeRe = regexp.MustCompile(`\bname\s*:\s*["'](unreal_\w+)["']`)
)

// The toolkit-registry lint (k18): every tool a shipped skill names in
// toolkit_tools must exist in the UE LLM Toolkit, registered in its
// MCPToolRegistry.cpp or defined by its mcp-bridge (get_ue_context, status).
// It is the drift alarm for upstream renames; when it fires, the skill is
// wrong, not the lint. Without a toolkit checkout it skips.
func TestToolkitToolsExistInToolkit(t *testing.T) {
	dir := toolkitCheckout(t)
	registered, bridge, err := toolkitToolNames(os.DirFS(dir))
	if err != nil {
		t.Fatalf("UE LLM Toolkit at %s: %v", dir, err)
	}
	t.Logf("UE LLM Toolkit at %s: %d tools registered in MCPToolRegistry.cpp, %d defined by the mcp-bridge (%s)",
		dir, len(registered), len(bridge), strings.Join(slices.Sorted(maps.Keys(bridge)), ", "))

	all, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	for _, s := range all {
		for _, tool := range s.ToolkitTools {
			if !registered[tool] && !bridge[tool] {
				t.Errorf("skill %q: toolkit_tools names %q, which the UE LLM Toolkit neither registers in MCPToolRegistry.cpp nor defines in its mcp-bridge", s.Name, tool)
			}
		}
	}
	if t.Failed() {
		known := slices.Sorted(maps.Keys(registered))
		known = append(known, slices.Sorted(maps.Keys(bridge))...)
		t.Logf("the toolkit at %s provides: %s", dir, strings.Join(known, ", "))
	}
}

// toolkitCheckout returns the UE LLM Toolkit checkout the lint runs against,
// or skips the test when there is none. $UE_LLM_TOOLKIT wins when set; a
// relative value is taken from the repository root. Otherwise the checkout is
// ue-llm-toolkit next to the repository root, or next to the main checkout
// when this is a linked git worktree such as .claude/worktrees/<name>.
func toolkitCheckout(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("repository root: %v", err)
	}
	hasRegistry := func(dir string) bool {
		_, err := os.Stat(filepath.Join(dir, filepath.FromSlash(toolkitRegistry)))
		return err == nil
	}
	if dir := os.Getenv("UE_LLM_TOOLKIT"); dir != "" {
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(root, dir)
		}
		if !hasRegistry(dir) {
			t.Skipf("UE_LLM_TOOLKIT=%s holds no %s; point it at a UE LLM Toolkit checkout to run the toolkit-registry lint", dir, toolkitRegistry)
		}
		return dir
	}
	var tried []string
	for _, r := range []string{root, mainCheckout(root)} {
		dir := filepath.Join(filepath.Dir(r), "ue-llm-toolkit")
		if slices.Contains(tried, dir) {
			continue
		}
		if hasRegistry(dir) {
			return dir
		}
		tried = append(tried, dir)
	}
	t.Skipf("no UE LLM Toolkit checkout at %s; set UE_LLM_TOOLKIT to one to run the toolkit-registry lint", strings.Join(tried, " or "))
	return ""
}

// mainCheckout returns the main working tree of the repository rooted at
// root. For a linked worktree, .git is a file "gitdir: <dir>" naming its
// directory under the main .git/worktrees/, and that directory's commondir
// file leads back to the main .git. Anything else (an ordinary clone, a
// submodule, a bare main repository) returns root.
func mainCheckout(root string) string {
	data, err := os.ReadFile(filepath.Join(root, ".git"))
	if err != nil { // a directory, or no git at all
		return root
	}
	gitdir, ok := strings.CutPrefix(strings.TrimSpace(string(data)), "gitdir:")
	if !ok {
		return root
	}
	gitdir = strings.TrimSpace(gitdir)
	if !filepath.IsAbs(gitdir) {
		gitdir = filepath.Join(root, gitdir)
	}
	data, err = os.ReadFile(filepath.Join(gitdir, "commondir"))
	if err != nil {
		return root
	}
	common := strings.TrimSpace(string(data))
	if !filepath.IsAbs(common) {
		common = filepath.Join(gitdir, common)
	}
	if filepath.Base(common) != ".git" {
		return root
	}
	return filepath.Dir(common)
}

// toolkitToolNames reads a toolkit checkout: the names of the tools
// MCPToolRegistry.cpp registers, and of those the mcp-bridge defines itself,
// without its unreal_ prefix. The registry names classes; class FMCPTool_X
// declares its name in Tools/MCPTool_X.h or Tools/MCPTool_X.cpp, the files the
// registry includes. A registration under #if counts: it exists in some
// engine version. Anything the parse cannot place is an error, so a change of
// upstream syntax stops the lint instead of thinning what it checks.
func toolkitToolNames(fsys fs.FS) (registered, bridge map[string]bool, err error) {
	src, err := fs.ReadFile(fsys, toolkitRegistry)
	if err != nil {
		return nil, nil, err
	}
	classes := registerRe.FindAllStringSubmatch(stripComments(string(src)), -1)
	if len(classes) == 0 {
		return nil, nil, fmt.Errorf("%s: no RegisterTool(MakeShared<FMCPTool_…>()) found; if the registration syntax changed, adapt the lint", toolkitRegistry)
	}
	registered = map[string]bool{}
	for _, m := range classes {
		stem := path.Join(toolkitMCP, "Tools", strings.TrimPrefix(m[1], "F"))
		names := map[string]bool{}
		for _, ext := range []string{".h", ".cpp"} {
			src, err := fs.ReadFile(fsys, stem+ext)
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			if err != nil {
				return nil, nil, err
			}
			for _, n := range toolNameRe.FindAllStringSubmatch(stripComments(string(src)), -1) {
				names[n[1]] = true
			}
		}
		if len(names) != 1 {
			return nil, nil, fmt.Errorf("%s: want one Info.Name = TEXT(\"…\") in %s.h or %[2]s.cpp, found %d %v",
				m[1], stem, len(names), slices.Sorted(maps.Keys(names)))
		}
		maps.Copy(registered, names)
	}

	src, err = fs.ReadFile(fsys, toolkitBridge)
	if err != nil {
		return nil, nil, err
	}
	bridge = map[string]bool{}
	for _, m := range bridgeRe.FindAllStringSubmatch(stripComments(string(src)), -1) {
		bridge[strings.TrimPrefix(m[1], "unreal_")] = true
	}
	if len(bridge) == 0 {
		return nil, nil, fmt.Errorf(`%s: no tool defined as name: "unreal_…" found; if the bridge's syntax changed, adapt the lint`, toolkitBridge)
	}
	return registered, bridge, nil
}

// stripComments drops the // and /* */ comments of C++ or JavaScript source,
// so a registration or tool commented out upstream does not count. String
// literals ("…", '…', `…`) are copied untouched: a description such as
// "/Game/*" must not open a comment. A quote right after a letter or digit is
// a C++14 digit separator (1'000), not a literal. C++ raw strings (R"(…)") are
// not handled; the toolkit has none.
func stripComments(src string) string {
	var b strings.Builder
	for i := 0; i < len(src); i++ {
		c := src[i]
		switch {
		case strings.HasPrefix(src[i:], "//"):
			n := strings.IndexByte(src[i:], '\n')
			if n < 0 {
				return b.String()
			}
			i += n - 1 // keep the newline
		case strings.HasPrefix(src[i:], "/*"):
			n := strings.Index(src[i+2:], "*/")
			if n < 0 {
				return b.String()
			}
			b.WriteByte(' ')
			i += n + 3
		case c == '"' || c == '`' || c == '\'' && (i == 0 || !isWordByte(src[i-1])):
			j := i + 1
			for j < len(src) && src[j] != c {
				if src[j] == '\\' {
					j++
				}
				j++
			}
			end := min(j+1, len(src))
			b.WriteString(src[i:end])
			i = end - 1
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

func isWordByte(c byte) bool {
	return c == '_' || '0' <= c && c <= '9' || 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z'
}

// The lint's parse runs in CI too, over a stand-in checkout that holds what
// the real one does and what it must not count: a registration under #if, a
// name declared in the .cpp, commented-out registrations and bridge tools, a
// tool header nothing registers. Each comment opener inside a literal, and
// each digit separator, stands before a name it would swallow or a
// commented-out one it would keep, so a stripper that mistakes it fails here.
func TestToolkitToolNamesParsesRegistryAndBridge(t *testing.T) {
	fsys := fstest.MapFS{
		toolkitRegistry: md(`#include "Tools/MCPTool_Alpha.h"
void FMCPToolRegistry::RegisterBuiltinTools()
{
	RegisterTool(MakeShared<FMCPTool_Alpha>());
#if ENGINE_MAJOR_VERSION == 5 && ENGINE_MINOR_VERSION >= 7
	RegisterTool( MakeShared< FMCPTool_Beta >(TaskQueue) );
#endif
	// RegisterTool(MakeShared<FMCPTool_Gone>());
	/* RegisterTool(MakeShared<FMCPTool_Gone>()); */
	UE_LOG(LogUnrealClaude, Log, TEXT("Registered %d MCP tools /* not a comment"), Tools.Num());
	RegisterTool(MakeShared<FMCPTool_Gamma>());
}
`),
		toolkitMCP + "/Tools/MCPTool_Alpha.h": md(`/** MCP Tool: alpha. */
class FMCPTool_Alpha : public FMCPToolBase
{
	virtual FMCPToolInfo GetInfo() const override
	{
		const int32 Limit = 1'000; // Info.Name = TEXT("alpha_old");
		const TCHAR Quote = '"'; // Info.Name = TEXT("alpha_quote");
		FMCPToolInfo Info;
		Info.Description = TEXT("Searches /Game/* for http://assets");
		Info.Name = TEXT("alpha");
		return Info;
	}
};
`),
		toolkitMCP + "/Tools/MCPTool_Beta.h":   md("class FMCPTool_Beta : public FMCPToolBase\n{\n\tvirtual FMCPToolInfo GetInfo() const override;\n};\n"),
		toolkitMCP + "/Tools/MCPTool_Beta.cpp": md("FMCPToolInfo FMCPTool_Beta::GetInfo() const\n{\n\tFMCPToolInfo Info;\n\tInfo.Name = TEXT(\"beta\");\n\treturn Info;\n}\n"),
		toolkitMCP + "/Tools/MCPTool_Gamma.h":  md("class FMCPTool_Gamma\n{\n\tFMCPToolInfo GetInfo() const { FMCPToolInfo Info; Info.Name = TEXT(\"gamma\"); return Info; }\n};\n"),
		toolkitMCP + "/Tools/MCPTool_Gone.h":   md("class FMCPTool_Gone\n{\n\tFMCPToolInfo GetInfo() const { FMCPToolInfo Info; Info.Name = TEXT(\"gone\"); return Info; }\n};\n"),
		toolkitBridge: md("const server = new Server({ name: \"ue5-mcp-server\" });\n" +
			"tools: [{ description: \"// not a comment\", name: \"unreal_status\" }];\n" +
			"const mcpTools = unrealTools.map((tool) => ({ name: `unreal_${tool.name}`, description: `/* ${tool.description}` }));\n" +
			"// mcpTools.push({ name: \"unreal_old\" });\n" +
			"mcpTools.push({ description: \"/* not a comment\", name: 'unreal_get_ue_context' });\n"),
	}
	registered, bridge, err := toolkitToolNames(fsys)
	if err != nil {
		t.Fatalf("toolkitToolNames: %v", err)
	}
	if want := map[string]bool{"alpha": true, "beta": true, "gamma": true}; !maps.Equal(registered, want) {
		t.Errorf("registered = %v, want %v", registered, want)
	}
	if want := map[string]bool{"status": true, "get_ue_context": true}; !maps.Equal(bridge, want) {
		t.Errorf("bridge = %v, want %v", bridge, want)
	}
}

// What the parse cannot place stops the lint with the file and the cause,
// rather than shrinking the set of known tools until it checks nothing.
func TestToolkitToolNamesRejectsWhatItCannotPlace(t *testing.T) {
	const (
		header = toolkitMCP + "/Tools/MCPTool_Alpha.h"
		reg    = "RegisterTool(MakeShared<FMCPTool_Alpha>());\n"
		alpha  = "Info.Name = TEXT(\"alpha\");\n"
		bridge = "name: \"unreal_status\",\n"
	)
	cases := []struct {
		name  string
		files fstest.MapFS
		want  string
	}{
		{"no registry", fstest.MapFS{header: md(alpha), toolkitBridge: md(bridge)}, "MCPToolRegistry.cpp"},
		{"no registration", fstest.MapFS{toolkitRegistry: md("// RegisterTool(MakeShared<FMCPTool_Alpha>());\n"), header: md(alpha), toolkitBridge: md(bridge)}, "no RegisterTool"},
		{"registered class without a source file", fstest.MapFS{toolkitRegistry: md(reg), toolkitBridge: md(bridge)}, "FMCPTool_Alpha: want one Info.Name"},
		{"registered class without a name", fstest.MapFS{toolkitRegistry: md(reg), header: md("// " + alpha), toolkitBridge: md(bridge)}, "found 0"},
		{"registered class with two names", fstest.MapFS{toolkitRegistry: md(reg), header: md(alpha + "Info.Name = TEXT(\"beta\");\n"), toolkitBridge: md(bridge)}, "found 2"},
		{"no bridge", fstest.MapFS{toolkitRegistry: md(reg), header: md(alpha)}, "index.js"},
		{"bridge defines no tool", fstest.MapFS{toolkitRegistry: md(reg), header: md(alpha), toolkitBridge: md("// " + bridge)}, `name: "unreal_…"`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, _, err := toolkitToolNames(c.files)
			if err == nil {
				t.Fatal("toolkitToolNames succeeded, want an error")
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("error %q does not mention %q", err, c.want)
			}
		})
	}
}

// The sibling fallback has to work from a linked worktree such as
// .claude/worktrees/<name>, whose own parent holds no toolkit checkout.
func TestMainCheckoutFollowsLinkedWorktree(t *testing.T) {
	base := t.TempDir()
	repo := filepath.Join(base, "unreagent")
	gitdir := filepath.Join(repo, ".git", "worktrees", "wt")
	wt := filepath.Join(repo, ".claude", "worktrees", "wt")
	sub := filepath.Join(base, "sub")
	for _, d := range []string{gitdir, wt, filepath.Join(wt, "rel"), sub} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	relGitdir, err := filepath.Rel(filepath.Join(wt, "rel"), gitdir)
	if err != nil {
		t.Fatal(err)
	}
	write := func(p, s string) {
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(gitdir, "commondir"), "../..\n")
	write(filepath.Join(wt, ".git"), "gitdir: "+gitdir+"\n")
	write(filepath.Join(wt, "rel", ".git"), "gitdir: "+relGitdir+"\n") // worktree.useRelativePaths
	write(filepath.Join(sub, ".git"), "gitdir: ../unreagent/.git/modules/sub\n")

	cases := []struct{ name, root, want string }{
		{"ordinary clone", repo, repo},
		{"linked worktree", wt, repo},
		{"linked worktree with a relative gitdir", filepath.Join(wt, "rel"), repo},
		{"submodule", sub, sub},
		{"no git", base, base},
	}
	for _, c := range cases {
		if got := mainCheckout(c.root); got != c.want {
			t.Errorf("%s: mainCheckout(%s) = %s, want %s", c.name, c.root, got, c.want)
		}
	}
}
