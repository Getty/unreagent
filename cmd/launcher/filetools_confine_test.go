// Regression tests for k49 and k76: registerFileTools must keep every file
// tool inside files.root.
//
//   - k49: a symbolic link under the root that points outside it must not be
//     followed, for reading, listing, editing and writing, including writing
//     a file (or directories) that does not exist yet under a linked dir.
//   - k76: Windows reserved device names (CON, NUL, AUX, PRN, COM1-9, LPT1-9,
//     CONIN$, CONOUT$), in any path component and with any extension, are
//     rejected on every platform: on Windows they open a device instead of a
//     file, and a file of that name created on Linux breaks the project's
//     checkout on Windows. An absolute path is accepted only under the root;
//     any other absolute or rooted path is refused as not relative.
//
// Like filetools_test.go (whose scaffolding these tests reuse), the tools are
// driven through mcp.Server's JSON-RPC tools/call. The symlink tests skip
// where the OS refuses to create symlinks (unprivileged Windows); Windows
// junctions are not exercised here at all.
package main

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/conflict-industries/unreagent/internal/mcp"
)

// symlinkOrSkip creates newname -> oldname or skips the test when the OS
// does not allow it.
func symlinkOrSkip(t *testing.T, oldname, newname string) {
	t.Helper()
	if err := os.Symlink(oldname, newname); err != nil {
		t.Skipf("cannot create symlink %s -> %s: %v", newname, oldname, err)
	}
}

// outsideDir returns a fresh directory outside the tool root, holding
// secret.txt.
func outsideDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "secret.txt"), "outside secret\n")
	return dir
}

func assertNotExist(t *testing.T, path, what string) {
	t.Helper()
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Errorf("%s: %s was created (err=%v)", what, path, err)
	}
}

// --- k49: symlinks under the root that point out of it --------------------

func TestReadFileRejectsSymlinkedDirEscape(t *testing.T) {
	srv, root := newFileToolsServer(t, false)
	out := outsideDir(t)
	symlinkOrSkip(t, out, filepath.Join(root, "link"))

	res := callTool(t, srv, "read_file", map[string]interface{}{"path": "link/secret.txt"})

	if !res.IsError || strings.Contains(res.Text, "outside secret") {
		t.Errorf("read_file through a dir symlink out of the root: IsError=%v text=%q, want an error and no content", res.IsError, res.Text)
	}
}

func TestReadFileRejectsSymlinkedFileEscape(t *testing.T) {
	srv, root := newFileToolsServer(t, false)
	out := outsideDir(t)
	symlinkOrSkip(t, filepath.Join(out, "secret.txt"), filepath.Join(root, "leak.txt"))

	res := callTool(t, srv, "read_file", map[string]interface{}{"path": "leak.txt"})

	if !res.IsError || strings.Contains(res.Text, "outside secret") {
		t.Errorf("read_file through a file symlink out of the root: IsError=%v text=%q, want an error and no content", res.IsError, res.Text)
	}
}

func TestListDirRejectsSymlinkedDirEscape(t *testing.T) {
	srv, root := newFileToolsServer(t, false)
	out := outsideDir(t)
	symlinkOrSkip(t, out, filepath.Join(root, "link"))

	res := callTool(t, srv, "list_dir", map[string]interface{}{"path": "link"})

	if !res.IsError || strings.Contains(res.Text, "secret.txt") {
		t.Errorf("list_dir through a dir symlink out of the root: IsError=%v text=%q, want an error and no listing", res.IsError, res.Text)
	}
}

func TestEditFileRejectsSymlinkedFileEscape(t *testing.T) {
	srv, root := newFileToolsServer(t, false)
	out := outsideDir(t)
	symlinkOrSkip(t, filepath.Join(out, "secret.txt"), filepath.Join(root, "leak.txt"))

	res := callTool(t, srv, "edit_file", map[string]interface{}{
		"path":       "leak.txt",
		"old_string": "secret",
		"new_string": "EDITED",
	})

	if !res.IsError {
		t.Errorf("edit_file through a file symlink out of the root: IsError=false, want an error (text: %q)", res.Text)
	}
	if got := mustReadFile(t, filepath.Join(out, "secret.txt")); got != "outside secret\n" {
		t.Errorf("edit_file through a file symlink MODIFIED A FILE OUTSIDE THE ROOT: now %q", got)
	}
}

func TestWriteFileRejectsSymlinkedFileEscape(t *testing.T) {
	srv, root := newFileToolsServer(t, false)
	out := outsideDir(t)
	symlinkOrSkip(t, filepath.Join(out, "secret.txt"), filepath.Join(root, "leak.txt"))

	res := callTool(t, srv, "write_file", map[string]interface{}{"path": "leak.txt", "content": "clobbered"})

	if !res.IsError {
		t.Errorf("write_file through a file symlink out of the root: IsError=false, want an error (text: %q)", res.Text)
	}
	if got := mustReadFile(t, filepath.Join(out, "secret.txt")); got != "outside secret\n" {
		t.Errorf("write_file through a file symlink OVERWROTE A FILE OUTSIDE THE ROOT: now %q", got)
	}
}

// TestWriteFileRejectsNewFileUnderSymlinkedDir covers a path that does not
// exist yet: its parent is a link out of the root.
func TestWriteFileRejectsNewFileUnderSymlinkedDir(t *testing.T) {
	srv, root := newFileToolsServer(t, false)
	out := outsideDir(t)
	symlinkOrSkip(t, out, filepath.Join(root, "link"))

	res := callTool(t, srv, "write_file", map[string]interface{}{"path": "link/new.txt", "content": "x"})

	if !res.IsError {
		t.Errorf("write_file of a new file under a dir symlink out of the root: IsError=false, want an error (text: %q)", res.Text)
	}
	assertNotExist(t, filepath.Join(out, "new.txt"), "write_file link/new.txt")
}

// TestWriteFileRejectsNewDirsUnderSymlinkedDir covers missing intermediate
// directories below a link out of the root: write_file must not create them
// on the far side of the link.
func TestWriteFileRejectsNewDirsUnderSymlinkedDir(t *testing.T) {
	srv, root := newFileToolsServer(t, false)
	out := outsideDir(t)
	symlinkOrSkip(t, out, filepath.Join(root, "link"))

	res := callTool(t, srv, "write_file", map[string]interface{}{"path": "link/sub/dir/new.txt", "content": "x"})

	if !res.IsError {
		t.Errorf("write_file with new dirs under a dir symlink out of the root: IsError=false, want an error (text: %q)", res.Text)
	}
	assertNotExist(t, filepath.Join(out, "sub"), "write_file link/sub/dir/new.txt")
}

// TestWriteFileRejectsDanglingSymlinkEscape covers a link whose target out of
// the root does not exist yet: writing through it would create the target.
func TestWriteFileRejectsDanglingSymlinkEscape(t *testing.T) {
	srv, root := newFileToolsServer(t, false)
	out := outsideDir(t)
	symlinkOrSkip(t, filepath.Join(out, "notyet.txt"), filepath.Join(root, "dangling.txt"))

	res := callTool(t, srv, "write_file", map[string]interface{}{"path": "dangling.txt", "content": "x"})

	if !res.IsError {
		t.Errorf("write_file through a dangling symlink out of the root: IsError=false, want an error (text: %q)", res.Text)
	}
	assertNotExist(t, filepath.Join(out, "notyet.txt"), "write_file dangling.txt")
}

// TestRelativeSymlinkInsideRootStillWorks pins that confinement does not
// reject links that stay inside the root.
func TestRelativeSymlinkInsideRootStillWorks(t *testing.T) {
	srv, root := newFileToolsServer(t, false)
	mustWriteFile(t, filepath.Join(root, "real", "a.txt"), "inside\n")
	symlinkOrSkip(t, "real", filepath.Join(root, "alias"))

	if res := callTool(t, srv, "read_file", map[string]interface{}{"path": "alias/a.txt"}); res.IsError || res.Text != "inside\n" {
		t.Errorf("read_file through an in-root symlink: IsError=%v text=%q, want %q", res.IsError, res.Text, "inside\n")
	}
	if res := callTool(t, srv, "write_file", map[string]interface{}{"path": "alias/b.txt", "content": "new"}); res.IsError {
		t.Errorf("write_file through an in-root symlink: %q", res.Text)
	}
	if got := mustReadFile(t, filepath.Join(root, "real", "b.txt")); got != "new" {
		t.Errorf("write_file through an in-root symlink: real/b.txt = %q, want %q", got, "new")
	}
}

// TestFileToolsWorkWhenRootIsSymlink pins that a root reached through a
// symlink (a project dir behind a link) is still usable.
func TestFileToolsWorkWhenRootIsSymlink(t *testing.T) {
	target := t.TempDir()
	mustWriteFile(t, filepath.Join(target, "a.txt"), "hello")
	link := filepath.Join(t.TempDir(), "proj")
	symlinkOrSkip(t, target, link)
	srv := mcp.NewServer("filetools-test", "0", nil)
	registerFileTools(srv, link, false)

	if res := callTool(t, srv, "read_file", map[string]interface{}{"path": "a.txt"}); res.IsError || res.Text != "hello" {
		t.Errorf("read_file under a symlinked root: IsError=%v text=%q, want %q", res.IsError, res.Text, "hello")
	}
	if res := callTool(t, srv, "write_file", map[string]interface{}{"path": "sub/b.txt", "content": "x"}); res.IsError {
		t.Errorf("write_file under a symlinked root: %q", res.Text)
	}
}

// wantLinkEscape is the error text for a path whose resolution hits a link
// that is absolute or leads out of root; inner is Go's own os.Root error.
func wantLinkEscape(given, root, inner string) string {
	return `Error: path "` + given + `": a link on the path (symlink or junction) is absolute or leads outside the allowed root "` + root + `" (` + inner + `)`
}

// TestLinkEscapeErrorNamesPathCauseAndRoot pins the wording of a refused
// link: Go's bare "path escapes from parent" names neither the cause nor the
// root, so the agent would not know what to change.
func TestLinkEscapeErrorNamesPathCauseAndRoot(t *testing.T) {
	srv, root := newFileToolsServer(t, false)
	out := outsideDir(t)
	symlinkOrSkip(t, out, filepath.Join(root, "link"))
	absGiven := filepath.Join(root, "link", "secret.txt")

	for _, c := range []struct {
		tool  string
		args  map[string]interface{}
		given string
		inner string
	}{
		{"read_file", map[string]interface{}{"path": "link/secret.txt"}, "link/secret.txt", "openat link/secret.txt: path escapes from parent"},
		{"list_dir", map[string]interface{}{"path": "link"}, "link", "openat link: path escapes from parent"},
		{"write_file", map[string]interface{}{"path": "link/sub/new.txt", "content": "x"}, "link/sub/new.txt", "mkdirat link/sub: path escapes from parent"},
		// Go nests two PathErrors here; the sentinel sits two levels down.
		{"write_file", map[string]interface{}{"path": "link/new.txt", "content": "x"}, "link/new.txt", "mkdirat link: statat link: path escapes from parent"},
		{"edit_file", map[string]interface{}{"path": "link/secret.txt", "old_string": "a", "new_string": "b"}, "link/secret.txt", "openat link/secret.txt: path escapes from parent"},
		// An absolute path is named as given; Go's part names the relative one.
		{"read_file", map[string]interface{}{"path": absGiven}, absGiven, "openat " + filepath.Join("link", "secret.txt") + ": path escapes from parent"},
	} {
		res := callTool(t, srv, c.tool, c.args)
		if want := wantLinkEscape(c.given, root, c.inner); !res.IsError || res.Text != want {
			t.Errorf("%s %q: IsError=%v\n text=%q\n want=%q", c.tool, c.given, res.IsError, res.Text, want)
		}
	}
}

// TestAbsoluteSymlinkInsideRootIsRefused pins the os.Root rule kept after
// review: an absolute link is refused even when it points back into the root
// (as a junction always is on Windows).
func TestAbsoluteSymlinkInsideRootIsRefused(t *testing.T) {
	srv, root := newFileToolsServer(t, false)
	mustWriteFile(t, filepath.Join(root, "real", "a.txt"), "inside\n")
	symlinkOrSkip(t, filepath.Join(root, "real"), filepath.Join(root, "abs"))

	res := callTool(t, srv, "read_file", map[string]interface{}{"path": "abs/a.txt"})

	if want := wantLinkEscape("abs/a.txt", root, "openat abs/a.txt: path escapes from parent"); !res.IsError || res.Text != want {
		t.Errorf("read_file through an absolute in-root symlink: IsError=%v\n text=%q\n want=%q", res.IsError, res.Text, want)
	}
}

// TestExplainLinkEscapeKeepsWrappedError pins that the wrapped error stays
// reachable and that any other os.Root error passes through untouched. Both
// errors come from a real os.Root, not from a copy of Go's text.
func TestExplainLinkEscapeKeepsWrappedError(t *testing.T) {
	r, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	_, escape := r.Open("../x")
	wrapped := explainLinkEscape("../x", "/proj", escape)
	if wrapped == escape || !errors.Is(wrapped, escape) {
		t.Errorf("explainLinkEscape(%v) = %v, want a wrapper that unwraps to the os.Root error", escape, wrapped)
	}
	var pe *os.PathError
	if !errors.As(wrapped, &pe) || pe.Op != "openat" {
		t.Errorf("explainLinkEscape: errors.As *os.PathError = %v, want the openat PathError", pe)
	}

	_, missing := r.Open("missing.txt")
	if got := explainLinkEscape("missing.txt", "/proj", missing); got != missing || !errors.Is(got, fs.ErrNotExist) {
		t.Errorf("explainLinkEscape(%v) = %v, want the error unchanged", missing, got)
	}
}

// --- k76: absolute and rooted paths -----------------------------------------

// wantMustBeRelative is the error text for an absolute or rooted path that
// does not lie under root.
func wantMustBeRelative(given, root string) string {
	return `Error: path "` + given + `" must be relative to the project root "` + root + `"; an absolute path is accepted only under it`
}

// TestAbsolutePathUnderRootIsAccepted pins that an absolute path below the
// root is made relative to it rather than refused.
func TestAbsolutePathUnderRootIsAccepted(t *testing.T) {
	srv, root := newFileToolsServer(t, false)
	mustWriteFile(t, filepath.Join(root, "Source", "a.txt"), "inside\n")
	sep := string(filepath.Separator)

	for _, p := range []string{
		filepath.Join(root, "Source", "a.txt"),
		root + sep + "Source" + sep + ".." + sep + "Source" + sep + "a.txt", // not clean, still under the root
	} {
		if res := callTool(t, srv, "read_file", map[string]interface{}{"path": p}); res.IsError || res.Text != "inside\n" {
			t.Errorf("read_file %q: IsError=%v text=%q, want %q", p, res.IsError, res.Text, "inside\n")
		}
	}
	if res := callTool(t, srv, "write_file", map[string]interface{}{"path": filepath.Join(root, "new", "b.txt"), "content": "x"}); res.IsError {
		t.Errorf("write_file under the root by absolute path: %q", res.Text)
	}
	if got := mustReadFile(t, filepath.Join(root, "new", "b.txt")); got != "x" {
		t.Errorf("write_file under the root by absolute path: new/b.txt = %q, want %q", got, "x")
	}
	if res := callTool(t, srv, "edit_file", map[string]interface{}{"path": filepath.Join(root, "Source", "a.txt"), "old_string": "inside", "new_string": "edited"}); res.IsError {
		t.Errorf("edit_file under the root by absolute path: %q", res.Text)
	}
	if res := callTool(t, srv, "list_dir", map[string]interface{}{"path": root}); res.IsError || res.Text != "Source/\nnew/\n" {
		t.Errorf("list_dir of the root by absolute path: IsError=%v text=%q, want %q", res.IsError, res.Text, "Source/\nnew/\n")
	}
}

// TestAbsolutePathUnderRootStillChecksReservedNames pins that the mapping to
// a relative path does not skip the device-name check, and that the refusal
// says why rather than calling the path "not relative".
func TestAbsolutePathUnderRootStillChecksReservedNames(t *testing.T) {
	srv, root := newFileToolsServer(t, false)
	p := filepath.Join(root, "Source", "aux.h")
	if runtime.GOOS != "windows" {
		mustWriteFile(t, p, "device stand-in\n")
	}

	res := callTool(t, srv, "read_file", map[string]interface{}{"path": p})

	if want := `Error: path component "aux.h" is a reserved Windows device name`; !res.IsError || res.Text != want {
		t.Errorf("read_file %q: IsError=%v text=%q, want %q", p, res.IsError, res.Text, want)
	}
}

// TestAbsolutePathUnderSymlinkedRootIsAccepted pins both spellings of a root
// behind a link: the configured one and the resolved (real) one.
func TestAbsolutePathUnderSymlinkedRootIsAccepted(t *testing.T) {
	target := t.TempDir()
	mustWriteFile(t, filepath.Join(target, "a.txt"), "hello")
	link := filepath.Join(t.TempDir(), "proj")
	symlinkOrSkip(t, target, link)
	resolved, err := filepath.EvalSymlinks(link)
	if err != nil {
		t.Fatal(err)
	}
	srv := mcp.NewServer("filetools-test", "0", nil)
	registerFileTools(srv, link, false)

	for _, p := range []string{filepath.Join(link, "a.txt"), filepath.Join(resolved, "a.txt")} {
		if res := callTool(t, srv, "read_file", map[string]interface{}{"path": p}); res.IsError || res.Text != "hello" {
			t.Errorf("read_file %q under a symlinked root: IsError=%v text=%q, want %q", p, res.IsError, res.Text, "hello")
		}
	}
}

// TestAbsolutePathOutsideRootIsRefused covers absolute paths that are not
// under the root, including one that merely shares its string prefix.
func TestAbsolutePathOutsideRootIsRefused(t *testing.T) {
	srv, root := newFileToolsServer(t, false)
	out := outsideDir(t)
	sibling := root + "-sibling"
	mustWriteFile(t, filepath.Join(sibling, "a.txt"), "sibling secret\n")
	sep := string(filepath.Separator)

	for _, p := range []string{
		filepath.Join(out, "secret.txt"),
		filepath.Join(sibling, "a.txt"),
		root + sep + ".." + sep + "evil.txt",
	} {
		res := callTool(t, srv, "read_file", map[string]interface{}{"path": p})
		if want := wantMustBeRelative(p, root); !res.IsError || res.Text != want {
			t.Errorf("read_file %q: IsError=%v\n text=%q\n want=%q", p, res.IsError, res.Text, want)
		}
	}
	evil := root + sep + ".." + sep + "evil.txt"
	if res := callTool(t, srv, "write_file", map[string]interface{}{"path": evil, "content": "x"}); !res.IsError {
		t.Errorf("write_file %q: IsError=false, want an error (text: %q)", evil, res.Text)
	}
	assertNotExist(t, filepath.Join(filepath.Dir(root), "evil.txt"), "write_file "+evil)
}

// TestRootedPathIsRefused covers a path that starts at the filesystem (or,
// on Windows, the drive) root. It used to be re-rooted under files.root.
func TestRootedPathIsRefused(t *testing.T) {
	srv, root := newFileToolsServer(t, false)
	p := "/Source/Foo.cpp"
	if runtime.GOOS == "windows" {
		p = `\Source\Foo.cpp`
	}

	for _, tool := range []string{"read_file", "write_file"} {
		res := callTool(t, srv, tool, map[string]interface{}{"path": p, "content": "x"})
		if want := wantMustBeRelative(p, root); !res.IsError || res.Text != want {
			t.Errorf("%s %q: IsError=%v\n text=%q\n want=%q", tool, p, res.IsError, res.Text, want)
		}
	}
	assertNotExist(t, filepath.Join(root, "Source"), "write_file "+p)
}

// --- list_dir output --------------------------------------------------------

// TestListDirListsEntriesSortedByName pins list_dir's output: one entry per
// line, directories with a trailing "/", sorted by name (byte order, so
// upper case first), the same for an empty, absent or "." path.
func TestListDirListsEntriesSortedByName(t *testing.T) {
	srv, root := newFileToolsServer(t, false)
	// Created in an order that is neither sorted nor reverse-sorted, so a
	// directory returning creation, reverse-creation or hash order does not
	// pass for sorted by accident.
	for _, name := range []string{"m.txt", "b/", "z.txt", "Q.txt", "a.txt", "y/", "c.txt"} {
		if strings.HasSuffix(name, "/") {
			if err := os.Mkdir(filepath.Join(root, name), 0o755); err != nil {
				t.Fatal(err)
			}
			continue
		}
		mustWriteFile(t, filepath.Join(root, name), "x")
	}
	const want = "Q.txt\na.txt\nb/\nc.txt\nm.txt\ny/\nz.txt\n"

	for _, args := range []map[string]interface{}{{"path": ""}, {}, {"path": "."}} {
		if res := callTool(t, srv, "list_dir", args); res.IsError || res.Text != want {
			t.Errorf("list_dir %v: IsError=%v\n text=%q\n want=%q", args, res.IsError, res.Text, want)
		}
	}
}

// --- k76: Windows reserved device names ------------------------------------

// reservedPaths are rejected by every file tool on every platform.
var reservedPaths = []string{
	"CON", "con", "PRN", "AUX", "Aux", "NUL", "nul",
	"COM1", "com9", "LPT1", "lpt9", "COM\u00b9", "LPT\u00b3",
	"CONIN$", "conout$",
	"aux.h", "NUL.txt", "con.tar.gz", "CON ", "con .txt", "nul.",
	"Source/aux.h", "Source/CON/x.txt", `Source\aux.h`, "a/b/LPT2.log",
}

// TestReadFileRejectsReservedDeviceNames creates each reserved name as a
// real file first (where the OS allows it, i.e. not on Windows), so the test
// fails if the tool merely happens to error on a missing file.
func TestReadFileRejectsReservedDeviceNames(t *testing.T) {
	srv, root := newFileToolsServer(t, false)
	for _, p := range reservedPaths {
		if runtime.GOOS != "windows" {
			mustWriteFile(t, filepath.Join(root, p), "device stand-in\n")
		}
		res := callTool(t, srv, "read_file", map[string]interface{}{"path": p})
		if !res.IsError || !strings.Contains(res.Text, "reserved Windows device name") {
			t.Errorf("read_file %q: IsError=%v text=%q, want a reserved-device-name error", p, res.IsError, res.Text)
		}
	}
}

func TestWriteFileRejectsReservedDeviceNames(t *testing.T) {
	srv, root := newFileToolsServer(t, false)
	for _, p := range reservedPaths {
		res := callTool(t, srv, "write_file", map[string]interface{}{"path": p, "content": "x"})
		if !res.IsError || !strings.Contains(res.Text, "reserved Windows device name") {
			t.Errorf("write_file %q: IsError=%v text=%q, want a reserved-device-name error", p, res.IsError, res.Text)
		}
		if runtime.GOOS != "windows" {
			assertNotExist(t, filepath.Join(root, p), "write_file "+p)
		}
	}
}

func TestEditFileRejectsReservedDeviceNames(t *testing.T) {
	srv, root := newFileToolsServer(t, false)
	for _, p := range []string{"aux.h", "Source/NUL", "COM1.ini"} {
		if runtime.GOOS != "windows" {
			mustWriteFile(t, filepath.Join(root, p), "foo")
		}
		res := callTool(t, srv, "edit_file", map[string]interface{}{"path": p, "old_string": "foo", "new_string": "bar"})
		if !res.IsError || !strings.Contains(res.Text, "reserved Windows device name") {
			t.Errorf("edit_file %q: IsError=%v text=%q, want a reserved-device-name error", p, res.IsError, res.Text)
		}
	}
}

func TestListDirRejectsReservedDeviceNames(t *testing.T) {
	srv, root := newFileToolsServer(t, false)
	for _, p := range []string{"CON", "Plugins/aux", "LPT1.d"} {
		if runtime.GOOS != "windows" {
			if err := os.MkdirAll(filepath.Join(root, p), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		res := callTool(t, srv, "list_dir", map[string]interface{}{"path": p})
		if !res.IsError || !strings.Contains(res.Text, "reserved Windows device name") {
			t.Errorf("list_dir %q: IsError=%v text=%q, want a reserved-device-name error", p, res.IsError, res.Text)
		}
	}
}

// TestReservedNameLookalikesAreAllowed pins that only the device names
// themselves are refused, not names that merely start with one.
func TestReservedNameLookalikesAreAllowed(t *testing.T) {
	srv, root := newFileToolsServer(t, false)
	for _, p := range []string{
		"CONTRIBUTING.md", "console.h", "Nullable.h", "auxiliary.txt", "prn_util.cpp",
		"COM", "COM10.txt", "LPTX", "CONIN", "CON_",
		// No COM0/LPT0 device exists; Go's filepath.IsLocal accepts them too.
		"COM0", "com0.txt", "LPT0", "lpt0.log",
		"Source/Console/a.h", "Content/Auxiliary/b.uasset",
	} {
		res := callTool(t, srv, "write_file", map[string]interface{}{"path": p, "content": "ok"})
		if res.IsError {
			t.Errorf("write_file %q: %q, want success", p, res.Text)
			continue
		}
		if got := mustReadFile(t, filepath.Join(root, p)); got != "ok" {
			t.Errorf("write_file %q: content %q, want %q", p, got, "ok")
		}
		if res := callTool(t, srv, "read_file", map[string]interface{}{"path": p}); res.IsError || res.Text != "ok" {
			t.Errorf("read_file %q: IsError=%v text=%q, want %q", p, res.IsError, res.Text, "ok")
		}
	}
}
