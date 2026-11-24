# unreagent build commands: what a reply cuts

The two reply shapes are in the skill itself, "Calling it". These are the
remaining items of its list "Reading the result".

- **A successful reply holds at most 48 KiB**, the `exit` line and the
  markers included. When the last 300 lines are longer (about 160 bytes a
  line on average), their oldest bytes go as well, and
  `[... B bytes omitted; a reply holds at most 48 KiB]` follows the `exit`
  line and any lines marker. Whole lines go first, then the start of the
  oldest line kept: you see at most 300 lines, and the first one can start
  mid-line. A single line longer than 48 KiB comes back as its end only. The
  cut never splits a UTF-8 character. The B bytes are gone.
- **A failing reply holds the start and the end.** When the `exit` line and
  the output are more than 10,000 bytes, the head is the output's first whole
  lines within 4 KiB, then comes `[... N lines omitted]`, then the tail: as
  many of the last whole lines as fit in the rest, about 5.8 KB when the head
  is full. N counts every line between head and tail. They are gone. No
  300-line limit applies to a failing reply.
- **A failing reply cuts a line only when it is longer than its share.** A
  first line over 4 KiB leaves the head as its start; a last line that does
  not fit leaves the tail as its end. The marker then counts bytes instead:
  `[... B bytes omitted; a failing call's reply holds at most 10 KB]`. The
  cut never splits a UTF-8 character.
- **Past 2 MiB the middle is dropped as well.** While the command runs, the
  launcher keeps only the first and the last MiB of its output, and the
  markers count the dropped middle too. In a failing reply, the head comes
  from the first MiB and the tail from the last, so the marker between them
  stands where the middle was dropped, and N (or B) includes it. In a
  successful reply, N whole lines, then B bytes, are everything above what
  you see. When the last MiB holds more than 300 lines
  (its first line, which can begin in the dropped part, counts as one), the
  300 lines all come from it: N includes the dropped lines, and B counts bytes
  of the 300 only. When it holds 300 or fewer, the lines reach back into the
  first MiB, at least to its last line: N counts only the first MiB's lines
  above them, and B includes the dropped bytes. In the rare reply that still
  shows the launcher's own `[... D bytes omitted]` line, that line names the
  middle, B leaves it out, and the reply can hold up to 302 lines, that line
  included. A failing reply shows that line only when the output ends in
  nearly a MiB of line breaks, with no text after them; the marker above it
  then leaves the middle out.
