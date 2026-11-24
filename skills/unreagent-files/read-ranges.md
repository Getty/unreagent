# read_file: range edges and offsets at the end of the file

The trailer line and paging are described in the skill itself, under
`read_file`.

Range edges move inward to UTF-8 character boundaries, by up to 3 bytes: a
start inside a character moves forward past its continuation bytes, an end
inside one moves back to the character's first byte. The trailer reports the
range after the move. Neither edge moves at the start or the end of the file,
so a file returned whole comes back byte for byte, a UTF-8 byte order mark at
offset 0 included (it arrives as U+FEFF, the first character). A range that
starts at offset 1 or 2 begins after it, at 3. One case leaves a character
cut: when it begins at the range's first byte once the start has moved, the end
stays, because moving it back would empty the range. That takes fewer than 4
bytes left after the start moved, so a `limit` below 7, or below 4 from a
character boundary.

An offset past the end is an error that names the file size. An offset exactly
at the end is not: it returns an empty range followed by `[bytes Z-Z of Z]`.
