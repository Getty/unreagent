// Tests for k73: read_file reads a byte range, chosen with offset (negative
// counts from the end) and limit (default and maximum 47 KiB since k173, the
// 48 KiB reply cap less room for the trailer), without loading the whole
// file. When the text is not the whole file it is followed by a trailer line
// "[bytes X-Y of Z]" (Y exclusive), plus the offset to continue with when
// more follows. Range edges move to UTF-8 character boundaries. Like
// filetools_test.go, whose scaffolding these tests reuse, the tool is driven
// through mcp.Server's JSON-RPC tools/call.
package main

import (
	"fmt"
	"math"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// numberedLines returns n lines of eight bytes each ("0000000\n",
// "0000001\n", …), so every byte offset of the content is predictable.
func numberedLines(n int) string {
	var sb strings.Builder
	for i := range n {
		fmt.Fprintf(&sb, "%07d\n", i)
	}
	return sb.String()
}

// readFileTool writes content to name under a fresh root and calls read_file
// on it with the given extra arguments.
func readFileTool(t *testing.T, name, content string, extra map[string]interface{}) toolCallResult {
	t.Helper()
	srv, root := newFileToolsServer(t, true)
	mustWriteFile(t, filepath.Join(root, name), content)
	args := map[string]interface{}{"path": name}
	for k, v := range extra {
		args[k] = v
	}
	return callTool(t, srv, "read_file", args)
}

func assertReadFile(t *testing.T, what string, res toolCallResult, want string) {
	t.Helper()
	if res.IsError {
		t.Fatalf("%s: unexpected error: %q", what, res.Text)
	}
	if res.Text != want {
		g, w := res.Text, want
		if len(g) > 200 || len(w) > 200 {
			// Big texts: report the lengths and both ends, not 47 KiB twice.
			t.Fatalf("%s: text mismatch\n len got=%d want=%d\n got  head=%q tail=%q\n want head=%q tail=%q",
				what, len(g), len(w), g[:min(60, len(g))], g[max(0, len(g)-80):], w[:min(60, len(w))], w[max(0, len(w)-80):])
		}
		t.Fatalf("%s:\n got  %q\n want %q", what, g, w)
	}
}

// TestReadFileSmallFileHasNoTrailer pins that a file returned whole comes back
// byte for byte with nothing appended — also when offset/limit were given but
// the range still covers the whole file. No trailer means "this is the whole
// file"; an empty file is the whole file too.
func TestReadFileSmallFileHasNoTrailer(t *testing.T) {
	const content = "line one\nline two\n"
	for _, tc := range []struct {
		name  string
		extra map[string]interface{}
	}{
		{"no range arguments", nil},
		{"offset 0", map[string]interface{}{"offset": 0}},
		{"limit larger than the file", map[string]interface{}{"limit": 1000}},
		{"negative offset larger than the file", map[string]interface{}{"offset": -1000}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assertReadFile(t, tc.name, readFileTool(t, "small.txt", content, tc.extra), content)
		})
	}
	t.Run("empty file", func(t *testing.T) {
		assertReadFile(t, "empty file", readFileTool(t, "empty.txt", "", nil), "")
	})
}

// TestReadFileLargeFileReturnsHeadWithTotal pins the default for a file
// larger than the limit: the first 47 KiB, then a trailer naming the total
// size and the offset that continues the read. The old tool returned the
// head with a bare "[truncated]" and no way on.
func TestReadFileLargeFileReturnsHeadWithTotal(t *testing.T) {
	content := numberedLines(131072) // 1 MiB
	want := content[:48128] + "\n[bytes 0-48128 of 1048576; continue with offset=48128]"
	assertReadFile(t, "1 MiB file, no offset", readFileTool(t, "big.log", content, nil), want)
}

// TestReadFileNegativeOffsetReadsTail pins the tail convenience: a negative
// offset counts from the end, so -48128, the example the description gives,
// is the last 47 KiB. The range ends at the end of the file, so the trailer
// has no "continue" part.
func TestReadFileNegativeOffsetReadsTail(t *testing.T) {
	content := numberedLines(131072) // 1 MiB
	want := content[1048576-48128:] + "\n[bytes 1000448-1048576 of 1048576]"
	assertReadFile(t, "offset=-48128", readFileTool(t, "big.log", content, map[string]interface{}{"offset": -48128}), want)

	// A tail of a small file: a range that does not start at 0 is not the
	// whole file, so it carries the trailer.
	assertReadFile(t, "offset=-9 on a small file",
		readFileTool(t, "small.txt", "line one\nline two\n", map[string]interface{}{"offset": -9}),
		"line two\n\n[bytes 9-18 of 18]")
}

// TestReadFileMiddleRange pins a range in the middle: offset and limit both
// apply, and the trailer names where to continue. A numeric string is
// accepted for both, as getInt does for every integer argument.
func TestReadFileMiddleRange(t *testing.T) {
	content := numberedLines(1000) // 8000 bytes
	want := content[1000:1500] + "\n[bytes 1000-1500 of 8000; continue with offset=1500]"
	assertReadFile(t, "offset=1000 limit=500",
		readFileTool(t, "mid.log", content, map[string]interface{}{"offset": 1000, "limit": 500}), want)
	assertReadFile(t, "offset/limit as strings",
		readFileTool(t, "mid.log", content, map[string]interface{}{"offset": "1000", "limit": "500"}), want)
}

// TestReadFileOffsetPastEndIsError pins that an offset beyond the end is an
// error naming the file size, not an empty success the agent could mistake
// for an empty file.
func TestReadFileOffsetPastEndIsError(t *testing.T) {
	res := readFileTool(t, "f.txt", "0123456789", map[string]interface{}{"offset": 11})
	want := "Error: 'offset' 11 is past the end of \"f.txt\" (10 bytes)"
	if !res.IsError || res.Text != want {
		t.Fatalf("offset past the end: IsError=%v\n got  %q\n want %q", res.IsError, res.Text, want)
	}
}

// TestReadFileOffsetAtEndIsEmptyRange pins that an offset exactly at the end
// is not an error: it returns an empty range with the trailer, so an agent
// paging through a file can tell "nothing more" from a failure.
func TestReadFileOffsetAtEndIsEmptyRange(t *testing.T) {
	assertReadFile(t, "offset == size",
		readFileTool(t, "f.txt", "0123456789", map[string]interface{}{"offset": 10}),
		"\n[bytes 10-10 of 10]")
}

// TestReadFileLimitIsClampedTo47KiB pins that a limit above 47 KiB is
// clamped rather than honoured, and that a limit that is not positive is an
// error instead of an empty read.
func TestReadFileLimitIsClampedTo47KiB(t *testing.T) {
	content := numberedLines(131072) // 1 MiB
	want := content[:48128] + "\n[bytes 0-48128 of 1048576; continue with offset=48128]"
	assertReadFile(t, "limit=10 MiB", readFileTool(t, "big.log", content, map[string]interface{}{"limit": 10 << 20}), want)

	for _, limit := range []int{0, -1} {
		res := readFileTool(t, "f.txt", "0123456789", map[string]interface{}{"limit": limit})
		want := fmt.Sprintf("Error: 'limit' must be a positive number of bytes, got %d", limit)
		if !res.IsError || res.Text != want {
			t.Errorf("limit=%d: IsError=%v\n got  %q\n want %q", limit, res.IsError, res.Text, want)
		}
	}
}

// trailerRE is read_file's trailer line, the continue offset optional.
var trailerRE = regexp.MustCompile(`\n\[bytes (\d+)-(\d+) of (\d+)(?:; continue with offset=(\d+))?\]$`)

// k173: read_file returned up to 256 KiB per call, more than the 48 KiB
// (toolReplyBytes) Claude Code takes into the agent's context from one reply.
// Its default and maximum range now leave room for the trailer line, at its
// longest too, and a file larger than one call is still read whole by
// following the trailer's offsets, each reply within the cap.
func TestReadFileReplyFitsTheCap(t *testing.T) {
	longest := fmt.Sprintf("\n[bytes %d-%d of %d; continue with offset=%d]", int64(math.MaxInt64), int64(math.MaxInt64), int64(math.MaxInt64), int64(math.MaxInt64))
	if readFileLimit+len(longest) > toolReplyBytes {
		t.Errorf("a range of %d bytes and the longest trailer (%d bytes) make a reply over the %d cap", readFileLimit, len(longest), toolReplyBytes)
	}

	srv, root := newFileToolsServer(t, true)
	if got := listTools(t, srv)["read_file"]["properties"].(map[string]interface{})["limit"].(map[string]interface{})["description"]; got != "Maximum number of bytes (default and maximum 48128)" {
		t.Errorf("limit description = %q, want it to name 48128", got)
	}

	// 160,000 bytes of lines, then 90,000 of 3-byte runes: the last ranges
	// end inside a character and have to move back.
	content := numberedLines(20000) + strings.Repeat("€", 30000)
	mustWriteFile(t, filepath.Join(root, "big.log"), content)
	for _, limit := range []interface{}{nil, 10 << 20} {
		t.Run(fmt.Sprintf("limit %v", limit), func(t *testing.T) {
			var got strings.Builder
			offset, calls := 0, 0
			for {
				args := map[string]interface{}{"path": "big.log", "offset": offset}
				if limit != nil {
					args["limit"] = limit
				}
				res := callTool(t, srv, "read_file", args)
				calls++
				if res.IsError {
					t.Fatalf("offset=%d: %q", offset, res.Text)
				}
				if len(res.Text) > toolReplyBytes {
					t.Fatalf("offset=%d: reply is %d bytes, more than the %d cap", offset, len(res.Text), toolReplyBytes)
				}
				m := trailerRE.FindStringSubmatchIndex(res.Text)
				if m == nil {
					t.Fatalf("offset=%d: no trailer: %.80q…", offset, res.Text[max(0, len(res.Text)-80):])
				}
				if x, _ := strconv.Atoi(res.Text[m[2]:m[3]]); x != offset {
					t.Fatalf("offset=%d: range starts at %d", offset, x)
				}
				got.WriteString(res.Text[:m[0]])
				if m[8] < 0 {
					break
				}
				offset, _ = strconv.Atoi(res.Text[m[8]:m[9]])
			}
			if got.String() != content {
				t.Fatalf("the ranges joined are %d bytes, not the file's %d", got.Len(), len(content))
			}
			// 250,000 bytes at 48,128 a call, less what the rune edges cost.
			if calls != 6 {
				t.Errorf("read in %d calls, want 6", calls)
			}
		})
	}
}

// TestReadFileMovesEdgesToRuneBoundaries pins that neither edge cuts a
// multi-byte character: a start inside a character moves forward past it, an
// end inside one moves back before it, and the trailer reports the range
// actually returned, so continuing from it loses nothing in UTF-8 text.
func TestReadFileMovesEdgesToRuneBoundaries(t *testing.T) {
	// "aa€bb€cc": € is E2 82 AC, at bytes 2-4 and 7-9; 12 bytes in all.
	const euro = "aa€bb€cc"
	for _, tc := range []struct {
		name    string
		content string
		extra   map[string]interface{}
		want    string
	}{
		{
			// [3,9) starts in the first € and ends in the second.
			"3-byte character straddling both edges", euro,
			map[string]interface{}{"offset": 3, "limit": 6},
			"bb\n[bytes 5-7 of 12; continue with offset=7]",
		},
		{
			// Continuing where the trailer said yields the second € whole.
			"continuing from the reported offset", euro,
			map[string]interface{}{"offset": 7, "limit": 6},
			"€cc\n[bytes 7-12 of 12]",
		},
		{
			// "a😀b": 😀 is F0 9F 98 80 at bytes 1-4. [2,6) starts on its
			// second byte, so the start moves forward by three bytes.
			"4-byte character at the start edge", "a😀b",
			map[string]interface{}{"offset": 2, "limit": 4},
			"b\n[bytes 5-6 of 6]",
		},
		{
			// [0,3) ends after its second byte, so the end moves back to 1.
			"4-byte character at the end edge", "a😀b",
			map[string]interface{}{"offset": 0, "limit": 3},
			"a\n[bytes 0-1 of 6; continue with offset=1]",
		},
		{
			// [0,4) ends after its third byte: its first byte is three
			// bytes back from the end, the farthest the end looks back.
			"4-byte character cut after its third byte", "a😀b",
			map[string]interface{}{"offset": 0, "limit": 4},
			"a\n[bytes 0-1 of 6; continue with offset=1]",
		},
		{
			// [0,5) ends right after the character: nothing to move.
			"end edge right after a complete character", "a😀b",
			map[string]interface{}{"offset": 0, "limit": 5},
			"a😀\n[bytes 0-5 of 6; continue with offset=5]",
		},
		{
			// The file itself ends inside a €: the end of the file never
			// moves, so the file still comes back whole and without a
			// trailer. The JSON encoding turns each stray byte into U+FFFD.
			"file ending inside a character", "ab\xE2\x82",
			nil,
			"ab\uFFFD\uFFFD",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assertReadFile(t, tc.name, readFileTool(t, "utf8.txt", tc.content, tc.extra), tc.want)
		})
	}
}

// TestReadFileTinyLimitStillAdvances pins the one exception to the rune
// boundaries: when moving the end back would empty the range, which happens
// only when fewer than 4 bytes remain after the start moved, the range keeps
// the cut character instead. Otherwise an agent continuing with offset=Y
// would get the same empty range forever.
func TestReadFileTinyLimitStillAdvances(t *testing.T) {
	res := readFileTool(t, "f.txt", "€x", map[string]interface{}{"limit": 2})
	const trailer = "\n[bytes 0-2 of 4; continue with offset=2]"
	if res.IsError || !strings.HasSuffix(res.Text, trailer) {
		t.Fatalf("limit=2 inside a 3-byte character: IsError=%v text=%q, want a 2-byte range ending in %q", res.IsError, res.Text, trailer)
	}

	// A limit of 4 can do it too, once the start has moved: "a😀😀" is
	// 61 F0 9F 98 80 F0 9F 98 80. [2,6) moves its start to 5, which leaves
	// only the first byte of the second 😀; the JSON encoding turns it into
	// U+FFFD.
	assertReadFile(t, "limit=4 with the start moved by 3",
		readFileTool(t, "f.txt", "a😀😀", map[string]interface{}{"offset": 2, "limit": 4}),
		"\uFFFD\n[bytes 5-6 of 9; continue with offset=6]")
}

// TestReadFileStartEdgeOnInvalidUTF8 pins the two limits of the start edge,
// which only bytes that are not UTF-8 reach: it never moves at offset 0, so
// a file that begins with a stray continuation byte still comes back whole,
// and it moves by at most 3 bytes, the most a character can have after its
// first byte, however many continuation bytes follow. The JSON encoding
// turns each stray byte into U+FFFD.
func TestReadFileStartEdgeOnInvalidUTF8(t *testing.T) {
	assertReadFile(t, "file starting with a continuation byte",
		readFileTool(t, "bin.dat", "\x80abc", nil),
		"\uFFFDabc")

	// Four continuation bytes at bytes 1-4: the start moves from 1 to 4,
	// not past all four.
	assertReadFile(t, "four continuation bytes at the start edge",
		readFileTool(t, "bin.dat", "a\x80\x80\x80\x80bc", map[string]interface{}{"offset": 1}),
		"\uFFFDbc\n[bytes 4-7 of 7]")
}

// TestReadFileKeepsBOMAtOffsetZero pins the UTF-8 byte order mark that UE
// logs start with: a range from offset 0 returns it untouched (U+FEFF as the
// first character, the trailer still counting from 0), and a range that
// starts inside it moves past it, like any other character.
func TestReadFileKeepsBOMAtOffsetZero(t *testing.T) {
	const content = "\xEF\xBB\xBFLog file open\n" // 3 + 14 = 17 bytes
	assertReadFile(t, "BOM file, whole", readFileTool(t, "ue.log", content, nil), content)
	assertReadFile(t, "BOM file, limit=8",
		readFileTool(t, "ue.log", content, map[string]interface{}{"limit": 8}),
		"\uFEFFLog f\n[bytes 0-8 of 17; continue with offset=8]")
	for _, offset := range []int{1, 2} {
		assertReadFile(t, fmt.Sprintf("BOM file, offset=%d", offset),
			readFileTool(t, "ue.log", content, map[string]interface{}{"offset": offset}),
			"Log file open\n\n[bytes 3-17 of 17]")
	}
}

// TestReadFileOnDirectoryIsError pins that reading a directory fails instead
// of returning an empty "file": a directory's reported size is 0 on Windows,
// which a size-driven range would otherwise turn into an empty success.
func TestReadFileOnDirectoryIsError(t *testing.T) {
	srv, root := newFileToolsServer(t, true)
	mustWriteFile(t, filepath.Join(root, "Source", "a.cpp"), "x")
	res := callTool(t, srv, "read_file", map[string]interface{}{"path": "Source"})
	want := "Error: path \"Source\" is a directory; use list_dir"
	if !res.IsError || res.Text != want {
		t.Fatalf("read_file on a directory: IsError=%v\n got  %q\n want %q", res.IsError, res.Text, want)
	}
}
