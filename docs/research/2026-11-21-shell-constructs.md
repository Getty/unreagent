# Document 3 — Catalogue of shell constructs that hide a command

## What the permission layer gets wrong if it assumes the obvious

- Splitting only at top-level separators misses commands in substitutions, redirections, arithmetic, functions and deferred handlers. Double quotes do not make Bash `$()` or PowerShell `$()` inert. [B-CS] [B-R] [B-SA] [P-Q] [P-TR]
- A command name need not occur contiguously, or occur anywhere, in the submitted bytes: lexical escapes and literal encodings can reconstruct it; variables, files and session state can supply it externally. [B-AC] [B-PE] [P-PS] [P-OP]
- A quoted argument can become a second program when consumed by `eval`, a shell, `Invoke-Expression` or a script-compilation API. The ordinary call operator `&` does **not** parse a string as an entire PowerShell command line. [B-B] [B-IN] [P-IEX] [P-CREATE] [P-OP]
- Alias timing is different: Bash reads/expands aliases before executing the containing input line; PowerShell resolves an alias when executing the later command. [B-AL] [P-AL] [P-PRE]

## Corpus contract

**Static** is a corpus expectation: recover nested command ASTs or reconstruct fully fixed literal text without executing the submitted code. It includes quote removal, known literal concatenation, Base64 decoding and bounded constant propagation. It does not mean **allow**. Every recovered command and its arguments remain subject to the rules, including code in conditional branches and deferred callbacks.

**Refuse** is the expected result whenever complete executable text, command identity, parser dialect, wrapper semantics or relevant runtime state cannot be established without running untrusted code. Locating one harmless marker is insufficient if other execution remains unresolved. Unsupported syntax also means refuse.

Default fixture state: a fresh shell process; the named builtins/cmdlets/wrappers resolve to their documented implementations; no unmentioned profiles, aliases, functions or hooks; the marker binaries are known. Any exception is stated in the row. Static results are conditional on that fixture state.

Examples are literal shell input, not host-language string literals. Multiline examples are the labelled fenced specimens immediately after their table; displayed line breaks are actual LF bytes. Table `\|` is Markdown escaping: the rendered input contains literal `|`, with no extra backslash. `id` and `whoami` are the only marker payloads. Other names in examples are language constructs, data producers or execution wrappers. Explicit fixture state is test data, not an assertion about the reader's machine.

Coverage is by grammar family and execution boundary, including their recursive combinations. Arbitrary external interpreters, extensions, callbacks and .NET APIs do not have a finite command-wrapper catalogue; unresolved ones belong to **refuse**. Bash command lookup and PowerShell command precedence are separate from recovering text. [B-CE] [P-PRE] [P-PROC]

## Bash 5.x and POSIX sh

Version labels distinguish POSIX.1-2017/2018 from POSIX.1-2024 where relevant. Bash-specific syntax must not be assumed for an unspecified `sh`; the two POSIX shell-language editions are the portability references. [POSIX18] [POSIX24]

| ID | Construct, grammar rule and version | Exact shell input | Hidden-command location | Static recovery / refusal |
|---|---|---|---|---|
| B01 | Command substitution runs the enclosed command list, including inside double quotes. **Bash 5.x; POSIX sh** [B-CS] | `: "$(id)"` | `id` inside `$(` … `)`; outer command is `:`. | Static: recursively parse substitution. |
| B02 | Backquote substitution; unescaped backquote closes it. Backslash is special before `$`, backquote and backslash. **Bash 5.x; POSIX sh** [B-CS] | `` : `id` `` | `id` between the two backquotes. | Static: use backquote quoting rules, not `$()` rules. |
| B03 | Nested backquote substitution escapes the inner backquotes. **Bash 5.x; POSIX sh** [B-CS] | `` : `: "\`id\`"` `` | `id` between escaped inner backquotes after outer-layer unescaping. | Static: remove exactly one relevant quoting layer, then recurse. |
| B04 | Bash 5.3 current-environment substitutions execute their bodies; the `\|` variant substitutes `REPLY` instead of captured stdout. **Bash 5.3+ only; not POSIX** [B-CS] [B53] | `: ${ id; }; : ${\| id; }` | Both `id` words inside `${ …; }` and `${\| …; }`. | Static with a 5.3 grammar; refuse as unsupported in a parser for earlier releases. |
| B05 | Input process substitution runs a list asynchronously and supplies a pathname. **Bash; requires FIFO or `/dev/fd` support; not POSIX** [B-PS] | `: <(id >&2)` | `id` inside `<(` … `)`. | Static; its output being unused does not remove the execution. |
| B06 | Output process substitution runs a list receiving the redirected stream. **Bash; platform support required; not POSIX** [B-PS] [B-R] | `: > >(id)` | `id` inside `>(` … `)`, after outer `>` redirection. | Static; distinguish the two redirection/substitution tokens. |
| B07 | An assignment-only simple command still expands its right-hand side. **Bash 5.x; POSIX sh** [B-SC] [POSIX24] | `x=$(id)` | `id` in the assignment value; no ordinary outer command name. | Static. |
| B08 | An unquoted here-document delimiter permits command substitution in its body. **Bash 5.x; POSIX sh** [B-R] [POSIX24] | [B08 exact input](#b08-exact-input) | `id` inside the here-document body, not the opening command line. | Static: parse the body with here-document expansion rules. |
| B09 | A quoted here-document is literal to the outer shell, but a shell receiving it as script input executes it. **Bash 5.x; POSIX sh** [B-R] [B-IN] [POSIX24] | [B09 exact input](#b09-exact-input) | `id` on the body line; quoted delimiter only disables the outer expansion pass. | Static only when the entire input and receiving shell grammar are fixed. |
| B10 | A here-string expands its word, including command substitutions. **Bash; not POSIX** [B-R] | `: <<<"$(id)"` | `id` in the word after `<<<`. | Static. |
| B11 | A here-string can supply literal script text to another Bash process. **Bash; not POSIX** [B-R] [B-IN] | `bash <<<'id'` | `id` inside the single-quoted stdin string. | Static: parse literal stdin as Bash code. |
| B12 | Redirection targets undergo expansion; a redirection is not inert pathname text. **Bash 5.x; POSIX sh** [B-R] [POSIX24] | `: >"$(id >&2)/dev/null"` | `id` in the filename word; its output is diverted to stderr, leaving `/dev/null`. | Static; inspect redirection words even for `:`. |
| B13 | A command consisting only of a redirection can execute a process substitution. **Bash; process substitution not POSIX** [B-R] [B-SC] [B-PS] | `< <(id >&2)` | `id` inside the redirection operand; there is no outer command word. | Static. |
| B14 | Arithmetic expansion first performs command substitution. **Bash 5.x; POSIX sh** [B-AE] [POSIX24] | `: "$(( $(id >&2) + 0 ))"` | `id` in `$()` inside `$((…))`; no numeric command output is required. | Static: parse expansion contexts before evaluating arithmetic. |
| B15 | Arithmetic variable values are expressions; indexed-array subscripts introduce another expansion context. **Bash; recursive array case not POSIX** [B-SA] [B-A] | `a=(0); x='a[$(id >&2)0]'; : "$((x))"` | `id` is initially single-quoted data assigned to `x`, later reached through arithmetic evaluation. | Static for this fully fixed value graph; refuse for inherited, mutated or unproved values. |
| B16 | Integer-attributed assignments evaluate arithmetic, including array expressions. **Bash; not POSIX** [B-SA] [B-BI] [B-A] | `a=(0); declare -i x='a[$(id >&2)0]'` | `id` inside the quoted `declare` assignment operand. | Static for the shown constants; refuse if the arithmetic expression/value graph is unresolved. |
| B17 | `let` takes arithmetic-expression arguments; shell quoting does not make those arguments inert. **Bash; not POSIX** [B-SA] [B-BI] [B-A] | `a=(0); let 'a[$(id >&2)0]=0'` | `id` inside a single-quoted argument to `let`. | Static for literal arithmetic input; refuse for unresolved expressions. |
| B18 | Indexed-array subscripts and substring offsets are arithmetic contexts. **Bash; arrays/substrings not POSIX** [B-A] [B-PE] | `a=(0); : "${a[$(id >&2)0]}"` | `id` in the subscript inside `${…}`. | Static for visible substitution; unresolved arithmetic names require refusal. |
| B19 | `[[…]]` expands operands; its arithmetic comparisons also evaluate arithmetic expressions. **Bash; not POSIX** [B-C] [B-SA] | `[[ x = "$(id)" ]]` | `id` inside the right operand, despite the enclosing conditional. | Static; finding a false condition is not permission to skip hidden syntax. |
| B20 | `case` expands the test word and patterns before matching. **Bash 5.x; POSIX sh** [B-C] [POSIX24] | `case x in "$(id)") :;; esac` | `id` inside a case pattern; not inside a case action. | Static. |
| B21 | A function body executes when the function name is invoked. **Bash 5.x; POSIX sh** [B-F] [POSIX24] | `f(){ id; }; f` | `id` in the definition body; `f` is the later invocation. | Static only with proven definition/resolution; otherwise refuse. |
| B22 | A function definition can carry redirections that run at invocation time. **Bash 5.x; POSIX sh** [B-F] | `f(){ :; } >"$(id >&2)/dev/null"; f` | `id` in the redirection attached after the function body. | Static; inspect attached redirections as well as the body. |
| B23 | Aliases expand while input is read. `eval` causes a new read after this same-line definition. **Bash example; alias timing is not runtime function dispatch** [B-AL] [B-B] | `shopt -s expand_aliases; alias m=id; eval m` | `id` is the alias replacement text, selected by the separately parsed `m`. | Static only with this fixed alias table and parser state; otherwise refuse. |
| B24 | `eval` joins its arguments with spaces and parses the result as shell code. **Bash 5.x; POSIX sh** [B-B] [POSIX24] | `eval 'id'` | `id` inside the quoted operand. | Static for completely literal reconstructed input; recursively parse that input. |
| B25 | `eval` can execute code absent from the command line. **Bash 5.x; POSIX sh** [B-B] [POSIX24] | `eval "$payload"` | No `id` bytes in input. Fixture: inherited `payload` contains exactly `id`. | Refuse: runtime string is unproved. |
| B26 | `.` reads and executes a file; Bash also spells it `source`. **Bash 5.x; POSIX sh** [B-B] [B-BI] [POSIX24] | `. ./marker.sh` | No `id` bytes in input. Fixture file `marker.sh` contains exactly `id` followed by LF. | Refuse for a command-line-only decision: executable bytes are external. |
| B27 | `sh -c` / `bash -c` parse a command-string operand; subsequent arguments are positional parameters, not more code. **Bash 5.x; POSIX sh** [B-IN] [POSIX24] | `sh -c 'id'` | `id` inside the command string after `-c`. | Static for the fixed inner string and a known shell dialect. |
| B28 | `exec` replaces the shell with its command operand. **Bash 5.x; POSIX sh** [B-B] [POSIX24] | `exec id` | `id` after the builtin and its options. | Static wrapper operand. |
| B29 | `command` invokes a command while suppressing shell-function lookup; `-v`/`-V` are queries instead. **Bash 5.x; POSIX sh** [B-BI] [POSIX24] | `command id` | `id` after `command`. | Static wrapper operand; preserve the query/invoke distinction. |
| B30 | `builtin` invokes a shell builtin, which may itself interpret code; `id` is not a stock Bash builtin. **Bash; not POSIX** [B-BI] [B-B] | `builtin eval 'id'` | `id` in the operand of the nested `eval` builtin. | Static: unwrap `builtin`, then apply `eval` grammar. |
| B31 | Bash `time` is a reserved-word pipeline prefix, not necessarily an external executable. **Bash reserved-word form; POSIX `time` utility is distinct** [B-P] | `time { id; }` | `id` in the timed brace group. | Static: parse the complete following pipeline. |
| B32 | `env` is an external utility; options and assignments precede its command. **Utility grammar; not Bash syntax** [ENV] | `env MARKER=1 id` | `id` after the environment assignment. | Static with `env` option grammar; unknown wrapper semantics require refusal. |
| B33 | GNU `env -S` splits its own string using its quote/escape/comment rules; it also supports `${VARNAME}` expansion. **GNU `env` with `-S`; not required by POSIX** [ENV] | `env -S 'id'` | `id` inside the `-S` operand, one outer-shell argument. | Static for this literal; refuse for unproved environment expansions or unsupported `-S` grammar. |
| B34 | `xargs` executes a command operand; replacement can also place input inside an interpreter command string. **`xargs` utility; here-string requires Bash** [XARGS] [B-R] | `xargs -I{} sh -c '{}' <<<'id'` | `id` in stdin, substituted into the `sh -c` operand, not an original executable word. | Static only if replacement input is fully fixed and both grammars are supported; otherwise refuse. |
| B35 | `find -exec` / `-execdir` consume an embedded argv up to `;`; the shell must pass that delimiter literally. **`-exec` POSIX utility; `-execdir` extension** [FIND] | `find . -prune -exec id \;` | `id` after `-exec`, before escaped `;`. | Static embedded argv; file-driven substitutions into code strings require refusal unless bounded. |
| B36 | The `find -exec … {} +` form batches path arguments, including after a nested shell command string. **POSIX `find` batching; utility grammar** [FIND+] [B-IN] | `find . -prune -exec sh -c 'id' _ {} +` | `id` inside the `sh -c` operand; `_` occupies the inner shell’s `$0`. | Static fixed inner code; appended paths do not become code in this example. |
| B37 | `nohup` invokes its command operand; it does not itself parse a shell program. **External utility** [NOHUP] | `nohup id </dev/null >/dev/null` | `id` after `nohup`. | Static wrapper operand. |
| B38 | `timeout` has options, then duration, then command argv. **GNU external utility; availability varies** [TIMEOUT] | `timeout 5 id` | `id` after the duration `5`. | Static wrapper operand. |
| B39 | `sudo` runs a command after option/assignment processing; `-n` avoids an authentication prompt. **External utility; not Windows sudo grammar** [SUDO] | `sudo -n -- id` | `id` after `--`. Fixture: policy permits this marker command without a password. | Static requested command; successful execution still depends on sudo policy. |
| B40 | `nice` options precede its command operand. **External utility** [NICE] | `nice -n 0 id` | `id` after adjustment argument `0`. | Static wrapper operand. |
| B41 | A subshell group contains a command list. **Bash 5.x; POSIX sh** [B-G] [POSIX24] | `(id)` | `id` between grouping parentheses. | Static. |
| B42 | A brace group contains a command list in the current shell. **Bash 5.x; POSIX sh** [B-G] [POSIX24] | `{ id; }` | `id` inside the braces. | Static. |
| B43 | Loop/conditional bodies and test lists are executable lists; their commands are not all top-level. **Bash 5.x; POSIX sh** [B-L] [B-C] [POSIX24] | `for x in 1; do id; done` | `id` in the loop body; the same recursive-list issue applies to `if`, `while` and `until`. | Static for syntactic commands; recover every potentially executed branch. |
| B44 | `coproc` launches the contained command asynchronously. **Bash; not POSIX** [B-CO] | `coproc { id; }; wait` | `id` in the coprocess body. | Static; background execution is still execution. |
| B45 | Trap action strings are parsed when the event occurs; `EXIT` runs when this shell exits. **Bash 5.x; POSIX sh** [B-B] [POSIX24] | `trap 'id' EXIT` | `id` in the quoted action argument, deferred until exit. | Static for literal action; refuse for generated/inherited actions. |
| B46 | Bash `DEBUG` is another deferred command hook. **Bash DEBUG/RETURN/ERR extensions; POSIX has ordinary signal traps** [B-B] | `trap 'id' DEBUG; :` | `id` inside the trap action; it executes before the following `:`. | Static for the fixed action; other trap types still require action inspection. |
| B47 | Brace expansion creates words before command-name selection. **Bash; not POSIX** [B-BR] [B-SC] | `{id,}` | `id` inside the brace-alternative expression. | Static expansion of finite literal alternatives. |
| B48 | Tilde expansion may produce a command pathname; `~+` expands `PWD`. **Bash `~+`; POSIX supports other tilde forms** [B-T] | `~+/id` | `id` is a path suffix. Fixture: current directory contains an executable marker named `id`. | Refuse unless expanded path and executable identity are fixed; spelling alone does not establish them. |
| B49 | An expanded first word becomes the command name; a variable is not automatically reparsed as shell grammar. **Bash 5.x; POSIX sh** [B-SC] [B-PE] [POSIX24] | `c=id; "$c"` | `id` in the assignment, referenced from the command position. | Static literal propagation here; refuse for an unresolved value. |
| B50 | Indirect parameter expansion follows the variable named by another parameter. **Bash; not POSIX** [B-PE] | `c=id; n=c; "${!n}"` | `id` in `c`; `${!n}` selects it via `n`. | Static only for this fixed indirection graph; otherwise refuse. |
| B51 | A nameref redirects variable access to another variable. **Bash 5.x; not POSIX** [B-BI] [B-PE] | `c=id; declare -n n=c; "$n"` | `id` in `c`; the command word resolves through nameref `n`. | Static only with fixed referents; otherwise refuse. |
| B52 | Array expansion can supply an entire argv, including its executable element. **Bash; not POSIX** [B-A] [B-SC] | `c=(id); "${c[@]}"` | `id` in array element zero. | Static for the literal array; refuse for unproved elements. |
| B53 | Adjacent quoted and unquoted parts form one command word after quote removal. **Bash 5.x; POSIX sh** [B-Q] [B-SC] [POSIX24] | `i''"d"` | No contiguous `id` token in source; concatenating the literal word parts yields `id`. | Static lexical reconstruction. |
| B54 | An unquoted backslash quotes the next ordinary character. **Bash 5.x; POSIX sh** [B-E] [POSIX24] | `\i\d` | No contiguous `id`; removing the quoting backslashes yields it. | Static lexical reconstruction. |
| B55 | Dollar-single-quote strings decode escape sequences, then act as quoted words. **Bash 5.x; standardized in POSIX.1-2024 §2.2.4, not POSIX.1-2017** [B-AC] [POSIX24] | `$'\151\144'` | No literal `id`; octal 151 and 144 decode to those bytes. | Static literal decoding; no shell execution is needed to decode. |
| B56 | Locale quoting performs translation; in locale C the dollar prefix is ignored. **Bash; not POSIX** [B-LC] | `$"id"` | `id` in the locale-quoted command word. Fixture: LC_ALL=C is inherited before Bash reads the input. | Static only for that pre-established C locale; otherwise refuse an unproved translation catalogue/environment. |
| B57 | A backslash immediately followed by LF is removed before tokenization. **Bash 5.x; POSIX sh** [B-E] [POSIX24] | [B57 exact input](#b57-exact-input) | The two command letters occupy different physical lines. | Static lexical reconstruction; trailing spaces after backslash change this input. |
| B58 | A comment ends at newline; following input remains executable. **Bash 5.x; POSIX sh** [B-CM] [POSIX24] | [B58 exact input](#b58-exact-input) | `id` after the LF, outside the comment. | Static; a comment is not a command-line terminator for the whole submitted string. |
| B59 | Parameter-operator words can contain substitutions; whether a branch is selected depends on the parameter. **Bash 5.x; POSIX sh** [B-PE] [POSIX24] | `unset marker; : "${marker:-$(id)}"` | `id` in the `:-` word. Other operator, pattern and replacement words need their own expansion rules. | Static visible nested command; refuse unproved command-valued results. |
| B60 | Filename expansion can generate a command pathname. **Bash 5.x; POSIX sh** [B-FN] [B-SC] [POSIX24] | `/usr/bin/[i]d` | `id` is reconstructed by the bracket pattern and filesystem match. | Refuse unless filesystem matches and selected executable are fixed. Fixture: path exists. |
| B61 | The tracing prompt `PS4` is expanded before trace output, so it can contain command substitutions. **Bash** [B-V] [B-CS] | `PS4='$(id >&2)'; set -x; :` | `id` in quoted `PS4` data, expanded later by tracing. | Static for this fixed prompt; refuse inherited/generated prompt code. |
| B62 | Noninteractive Bash can execute a startup file named by `BASH_ENV` before its requested command. **Bash; POSIX `ENV` has different invocation rules** [B-ST] [B-V] | `BASH_ENV=./marker.sh bash -c ':'` | No `id` in input. Fixture: `marker.sh` contains `id` plus LF. | Refuse: startup code is external to the submitted command string. |
| B63 | `mapfile -C` evaluates a callback; the callback function body may contain commands. **Bash; not POSIX** [B-BI] [B-F] | `f(){ id; }; mapfile -c 1 -C f <<<'x'` | `id` in `f`; `-C f` causes deferred invocation. | Static for fixed callback/body/input; otherwise refuse. |
| B64 | Command lookup can invoke `command_not_found_handle` when no ordinary command is found. **Bash; not POSIX** [B-CE] [B-F] | `command_not_found_handle(){ id; }; __unreagent_missing__` | `id` in the hook body. Fixture: `__unreagent_missing__` is absent from command lookup. | Static body; refuse if lookup or hook definitions are not fixed. |
| B65 | History expansion substitutes prior input in an enabled interactive history context. **Bash interactive history context; not POSIX** [B-H] | `!!` | No `id` in input. Fixture: immediately preceding history event is exactly `id`. | Refuse: the command line does not contain the relevant history state. |
| B66 | The `${parameter@P}` transformation expands the parameter value as a prompt string, including command substitution. **Bash 5.x; not POSIX** [B-PE] [B-PR] | `x='$(id >&2)'; : "${x@P}"` | `id` begins as single-quoted data and executes during the prompt transformation. | Static for the fixed value and prompt expansion rules; otherwise refuse. |
| B67 | Bash expands the inherited BASH_ENV value to obtain its startup filename; that expansion can itself execute code. **Bash; `--norc` does not disable this noninteractive path** [B-ST] [B-V] | `BASH_ENV='$(id >&2)' bash --noprofile --norc -c :` | `id` in the environment assignment; the child expands it before its `-c` body. | Static visible startup expansion; unproved startup-file results still require refusal. |
| B68 | A builtin array-element operand can cause arithmetic subscript evaluation after the outer quote layer is gone. **Bash; not POSIX** [B-A] [B-SA] [B-B] | `a=(x); unset 'a[$(id >&2)0]'` | `id` inside the quoted array lvalue passed to `unset`. | Static literal lvalue; refuse unresolved arithmetic/value graphs. |
| B69 | `fc -s` re-executes a selected history event; `history -s` can add the command as data first. **Bash history builtins; exact event-selection semantics matter** [B-HB] | `history -s id; fc -s id` | `id` is inserted as a history entry, then selected for execution. | Static only with the proven resulting history selection; otherwise refuse. |
#### B08 exact input

```bash
: <<END
$(id)
END
```

#### B09 exact input

```bash
sh <<'END'
id
END
```

#### B57 exact input

```bash
i\
d
```

#### B58 exact input

```bash
: # inert text
id
```

## PowerShell 5.1 and 7

| ID | Construct, grammar rule and version | Exact shell input | Hidden-command location | Static recovery / refusal |
|---|---|---|---|---|
| P01 | `Invoke-Expression` parses and executes its string argument in the current scope. **5.1 and 7** [P-IEX] | `Invoke-Expression 'whoami'` | `whoami` inside the quoted `-Command` positional operand. | Static for this literal; parse the recovered string recursively. |
| P02 | `iex` is the alias of `Invoke-Expression`; its command string can arrive through the pipeline. **5.1 and 7** [P-IEX] [P-PRE] | `'whoami' \| iex` | `whoami` in the upstream string expression. | Static with a fixed built-in alias and fixed pipeline value; otherwise refuse. |
| P03 | Call operator `&` invokes a command represented by a string; it does not parse that string as a whole script. **5.1 and 7** [P-OP] | `& 'whoami'` | `whoami` in the string following `&`. | Static command-name literal. |
| P04 | Dot invocation runs a script block in the current scope. **5.1 and 7** [P-SCOPE] [P-OP] | `. { whoami }` | `whoami` in the dot-invoked script block. | Static block. |
| P05 | Dot-sourcing can execute an external script file. **5.1 and 7** [P-SCOPE] | `. .\marker.ps1` | No `whoami` bytes in input. Fixture file contains exactly `whoami` and LF. | Refuse: script bytes are outside the command line. |
| P06 | A script block is executable when passed to `&`. **5.1 and 7** [P-SB] [P-OP] | `& { whoami }` | `whoami` in the invoked script block. | Static block; braces alone do not imply invocation. |
| P07 | A script block’s `.Invoke()` method executes its body. **5.1 and 7** [P-SBINV] | `({ whoami }).Invoke()` | `whoami` inside the script-block object, not a top-level command statement. | Static for this literal receiver; refuse for an unproved receiver. |
| P08 | `ScriptBlock.Create()` parses source text; `.Invoke()` then runs it. **5.1 and 7** [P-CREATE] [P-SBINV] | `[scriptblock]::Create('whoami').Invoke()` | `whoami` inside the argument to `Create`. | Static only for recognized API semantics and fixed source string; otherwise refuse. |
| P09 | Subexpression `$()` executes its statement list. **5.1 and 7** [P-OP] | `$null = $(whoami)` | `whoami` inside the assignment’s subexpression. | Static. |
| P10 | Expandable strings evaluate `$()` subexpressions. **5.1 and 7** [P-Q] | `"$(whoami)"` | `whoami` inside a string’s interpolation; the enclosing string is not inert. | Static. |
| P11 | Grouping parentheses can execute a pipeline and use its result as an expression. **5.1 and 7** [P-OP] | `(whoami)` | `whoami` inside grouping parentheses. | Static. |
| P12 | Array subexpression `@()` evaluates contained statements. **5.1 and 7** [P-OP] | `@(whoami)` | `whoami` inside `@(` … `)`. | Static. |
| P13 | Hashtable values can contain statement expressions, including a command pipeline. **5.1 and 7** [P-GRAM] | `@{x=whoami}` | `whoami` on the right of a hashtable key assignment. | Static. |
| P14 | Splatting supplies parameter values from a hashtable. **5.1 and 7** [P-SPL] [P-IEX] | `$p=@{Command='whoami'}; Invoke-Expression @p` | `whoami` in `p.Command`, bound to the execution cmdlet later. | Static fixed hashtable propagation; refuse unknown or mutated splats. |
| P15 | Aliases can refer to external commands; a same-line `Set-Alias` takes effect for the subsequent command. **5.1 and 7** [P-AL] [P-PRE] | `Set-Alias m whoami; m` | `whoami` in the alias definition; later command text is only `m`. | Static with proven successful definition and unchanged resolution; otherwise refuse. |
| P16 | Functions execute their stored statement blocks when invoked. **5.1 and 7** [P-F] [P-PRE] | `function m { whoami }; m` | `whoami` in the function body. | Static for the fixed resolved function; otherwise refuse. |
| P17 | Filters are pipeline-processing functions. **5.1 and 7** [P-F] | `filter m { whoami }; 1 \| m` | `whoami` in the filter definition. | Static for the fixed filter. |
| P18 | `ForEach-Object` invokes script-block arguments; `%` is its alias. **5.1 and 7** [P-FOR] | `1 \| % { whoami }` | `whoami` inside the pipeline callback. | Static with a fixed `%` resolution; otherwise refuse. |
| P19 | `Invoke-Command` executes a script-block argument; this example is local. **5.1 and 7** [P-INV] [P-SB] | `Invoke-Command { whoami }` | `whoami` inside the `-ScriptBlock` positional argument. | Static literal block; remote targets/session state are additional unproved inputs. |
| P20 | `Start-Job` schedules a script block for execution outside the foreground pipeline. **5.1 and 7** [P-JOB] | `Start-Job { whoami } \| Wait-Job \| Receive-Job` | `whoami` in the job script block. | Static nested body; asynchronous execution is not inert data. |
| P21 | `ForEach-Object -Parallel` executes a script block in parallel runspaces. **7+; not 5.1** [P-FOR] | `1 \| ForEach-Object -Parallel { whoami }` | `whoami` inside the parallel script-block argument. | Static block; unsupported on 5.1. |
| P22 | `Start-Process` launches its `FilePath`; `-ArgumentList` is another argument-transport layer. **5.1 and 7** [P-START] | `Start-Process whoami -Wait -NoNewWindow` | `whoami` is a parameter value, not the outer command name. | Static for a fixed executable and understood native transport. |
| P23 | `cmd /c` interprets its string with cmd.exe grammar; `/d` disables AutoRun commands. **5.1 and 7** [CMD] | `cmd.exe /d /c whoami` | `whoami` in the native interpreter’s command operand. | Static for this fixed simple inner command; refuse unsupported cmd grammar. |
| P24 | `--%` stops PowerShell parsing for native arguments; cmd.exe still interprets its own command text. **Windows; 5.1 and 7** [P-PARSE] [CMD] | `cmd.exe /d /c --% whoami` | `whoami` in the stop-parsing tail sent to cmd.exe. | Static only after both native argument transport and cmd grammar; `%NAME%` introduces environment dependence. |
| P25 | A nested Windows PowerShell executable accepts a command string. **Windows PowerShell 5.1 executable; callable from 5.1 or 7** [P-PS] | `powershell.exe -NoProfile -Command 'whoami'` | `whoami` in the `-Command` operand. | Static fixed inner source; native quotation transport remains a separate boundary. |
| P26 | `-EncodedCommand` uses Base64 of UTF-16LE source, not UTF-8. **Windows PowerShell 5.1** [P-PS] | `powershell.exe -NoProfile -EncodedCommand dwBoAG8AYQBtAGkA` | No literal `whoami`; decoding the operand as Base64 → UTF-16LE yields exactly `whoami`. | Static deterministic decoding, then parse; malformed/unsupported encodings require refusal. |
| P27 | PowerShell 7 `pwsh` also accepts `-EncodedCommand`. **7; host executable can be invoked by another shell** [P-PWSH] | `pwsh -NoProfile -EncodedCommand dwBoAG8AYQBtAGkA` | Same Base64 operand; decoded text is `whoami`. | Static deterministic decoding, then parse. |
| P28 | `-Command -` reads script text from stdin. **7 child; Windows PowerShell also documents `-Command -`** [P-PWSH] | `'whoami' \| pwsh -NoProfile -Command -` | `whoami` in the upstream string, interpreted by the child process. | Static only for fully known stdin; refuse unknown pipeline/file input. |
| P29 | A backtick before an otherwise ordinary character is removed; special escape letters have different results. **5.1 and 7** [P-LEX] | ``w`hoami`` | No contiguous `whoami`; backtick-h decodes to `h`. | Static lexical reconstruction; backtick-a would be BEL, not `a`. |
| P30 | A trailing backtick can continue input at a token boundary. **5.1 and 7** [P-SP] [P-LEX] | [P30 exact input](#p30-exact-input) | The quoted invocation target follows the continued physical line. | Static lexical reconstruction; whitespace after the backtick changes the result. |
| P31 | Literal string concatenation can supply a call-operator command name. **5.1 and 7** [P-OP] | `& ('who'+'ami')` | `whoami` is reconstructed from the two string literals. | Static constant reconstruction for known string operands; refuse arbitrary expressions. |
| P32 | Formatting and joining can likewise construct command strings. **5.1 and 7** [P-OP] | `& ('{0}{1}' -f 'who','ami')` | `whoami` is reconstructed by the literal format template. | Static only for the recognized bounded literal transform; otherwise refuse. |
| P33 | Character-number conversion and joining can construct the command without its literal letters. **5.1 and 7** [P-OP] [P-GRAM] | `& (-join [char[]](119,104,111,97,109,105))` | No `whoami` text: the six numeric character codes produce it. | Static if this exact literal transform is supported; arbitrary computation requires refusal. |
| P34 | Unicode escape sequences in expandable strings can hide command-name characters. **7 supports this; introduced in 6, absent in 5.1** [P-SP] | ``& "wh`u{6f}ami"`` | The `o` is encoded as Unicode 6f. | Static literal decoding. |
| P35 | A call-operator target may come from the environment. **5.1 and 7** [P-OP] | `& $env:MARKER` | No `whoami` text. Fixture: inherited `MARKER` equals `whoami`. | Refuse: external runtime value. |
| P36 | Pipeline-chain operators conditionally execute later pipelines. **7+ only; syntax error in 5.1** [P-CHAIN] | `'ok' && & 'whoami'` | `whoami` in the right-hand pipeline’s call-operator operand. | Static: recover both sides of `&&`/`\|\|`, not just the branch assumed to run. |
| P37 | A finally block is executable even when the try block contains no command. **5.1 and 7** [P-TRY] | `try {} finally { whoami }` | `whoami` in `finally`. | Static; likewise inspect try/catch bodies. |
| P38 | A trap statement supplies executable handling code for a terminating error. **5.1 and 7** [P-TR] | `trap { whoami; continue }; throw 'marker'` | `whoami` in the error handler. | Static trap body. |
| P39 | Parameter default expressions can execute before a function/script block’s body. **5.1 and 7** [P-F] [P-SB] | `& { param($x = $(whoami)) }` | `whoami` inside the parameter default, not the body. | Static default expression; body-only scanning misses it. |
| P40 | Conditional and loop statement blocks contain executable statements. **5.1 and 7** [P-GRAM] | `if ($true) { whoami }` | `whoami` in the conditional body. | Static; the same requirement covers loop bodies and executable test expressions. |
| P41 | A switch matching clause can itself be a script block, executed to test the input. **5.1 and 7** [P-SW] | `switch (1) { { whoami } {} }` | `whoami` in the predicate block, not the action block. | Static predicate and action blocks. |
| P42 | Class constructors execute statements when an instance is created. **5.1 and 7** [P-CLASS] | `class M { M(){ whoami } }; [M]::new()` | `whoami` in the constructor method. | Static known class body; unknown constructors/methods require refusal. |
| P43 | A ScriptProperty getter is code executed by property access. **5.1 and 7** [P-MEM] | `$o=[pscustomobject]@{}; Add-Member -InputObject $o -MemberType ScriptProperty -Name X -Value { whoami }; $o.X` | `whoami` in the getter block; the triggering expression is `$o.X`. | Static only for this fixed receiver/getter; unknown object access can require refusal. |
| P44 | The PowerShell SDK accepts a script string and executes it through `.Invoke()`. **5.1 and 7** [P-ADD] | `[powershell]::Create().AddScript('whoami').Invoke()` | `whoami` in an API argument; no command-invocation AST need name it. | Static only for recognized fixed API semantics; otherwise refuse. |
| P45 | A .NET method can start an executable directly. **5.1 and 7** [P-PROC] | `[System.Diagnostics.Process]::Start('whoami')` | `whoami` in the method argument. | Static only for the recognized overload and fixed argument; unknown methods/reflection require refusal. |
| P46 | Expandable here-strings evaluate subexpressions. **5.1 and 7** [P-Q] | [P46 exact input](#p46-exact-input) | `whoami` inside the here-string’s `$()`. | Static expansion; delimiter lines are part of the grammar. |
| P47 | A literal here-string can become code when passed to Invoke-Expression. **5.1 and 7** [P-Q] [P-IEX] | [P47 exact input](#p47-exact-input) | `whoami` in the otherwise literal here-string body. | Static: decode literal string, then parse as code. |
| P48 | Block comments end at the first `#>` and do not nest. **5.1 and 7** [P-COM] | `<# outer <# inner #> whoami` | `whoami` is outside the already closed comment. | Static lexical scanning; treating comments as nested would wrongly discard this command. |
| P49 | A single-line comment ends at LF, not at the end of all supplied input. **5.1 and 7** [P-COM] | [P49 exact input](#p49-exact-input) | `whoami` on the second physical line. | Static. |
| P50 | Start-Process joins an ArgumentList string array with spaces; PowerShell outer quotes are not carried into that joined string. **Windows; 5.1 and 7** [P-START] [CMD] | `Start-Process cmd.exe -ArgumentList '/d','/c','whoami' -Wait -NoNewWindow` | `whoami` in the third ArgumentList element, then in cmd.exe’s command string. | Static only with the join/quoting rules and the inner cmd grammar; otherwise refuse. |
| P51 | ValidateScript attributes run script blocks when validating a parameter or variable value. **5.1 and 7** [P-VALID] | `[ValidateScript({whoami; $true})][int]$x=1` | `whoami` inside an attribute argument; assignment triggers validation. | Static for the literal validation block; do not limit inspection to statement bodies. |
| P52 | A script-block replacement operand to `-replace` executes for each regex match. **7 supported; introduced in 6, not 5.1** [P-COMP] | `'x' -replace 'x',{whoami}` | `whoami` in the replacement callback, not in the regex pattern. | Static callback; this callback feature is unsupported in 5.1. |
| P53 | Calculated-property Expression entries execute script blocks for input objects. **5.1 and 7** [P-CALC] | `1 \| Select-Object @{Name='x';Expression={whoami}}` | `whoami` in a script block nested inside a hashtable argument. | Static with recognized consumer semantics and literal callback. |
| P54 | New-Module creates a dynamic module by running its script block. **5.1 and 7** [P-MOD] | `New-Module -ScriptBlock { whoami }` | `whoami` in module initialization code. | Static literal module initializer; exported members are further code/state. |
| P55 | A script-file `#Requires -Modules` directive can import a missing module before the script runs. **5.1 and 7** [P-REQ] | `& ./requires-marker.ps1` | No `whoami` text. Fixture: requires-marker.ps1 contains `#Requires -Modules MarkerModule` plus LF and `$null`; discoverable MarkerModule.psm1 contains `whoami` plus LF. | Refuse: required module initialization is external code. |
| P56 | `using module` imports a module via a declaration processed before ordinary statements. **5.1 and 7** [P-USING] | `using module ./MarkerModule.psm1` | No `whoami` text. Fixture: that module contains `whoami` plus LF. | Refuse: the command string does not establish module bytes/dependencies. |
| P57 | Array splatting supplies positional native arguments, including an interpreter command operand. **Windows; 5.1 and 7** [P-SPL] [CMD] | `$p='/d','/c','whoami'; cmd.exe @p` | `whoami` in the third array element, later passed to cmd.exe. | Static for the fixed array and recognized native transport; otherwise refuse. |
| P58 | Invoke-Expression may execute a runtime string absent from the source. **5.1 and 7** [P-IEX] | `Invoke-Expression $env:PAYLOAD` | No `whoami` bytes in input. Fixture: environment PAYLOAD is exactly `whoami`. | Refuse: unproved runtime source string. |
#### P30 exact input

```powershell
& `
'whoami'
```

#### P46 exact input

```powershell
@"
$(whoami)
"@
```

#### P47 exact input

```powershell
@'
whoami
'@ | Invoke-Expression
```

#### P49 exact input

```powershell
# inert text
whoami
```

## Constructs considered and unable to hide execution by themselves

- **Bash comments:** `# id` executes no marker. `: id#text` does not start a comment within that word and still executes no marker; the word is data to `:`. A following physical line is a separate matter, as B58 shows. [B-CM]
- **Bash literal quoting:** `: '$(id)'` and `: '$((id))'` do not execute `id`. Single quoting is not a defence when a later interpreter reparses the resulting string, as B15/B24 show. [B-Q] [B-B]
- **Bash literal data versus command position:** `: id`, `: {id,}`, `x=id` and `: ~` do not invoke the marker by themselves. Expansions can become executable names when used in command position, as B47–B52 show. [B-SC] [B-BR] [B-T]
- **Bash arithmetic identifier:** `$((id))` refers to a variable named `id`; it is not an instruction to run the external `id` command. Unproved variable/array expressions can nevertheless expose the B15 re-evaluation boundary. [B-SA] [B-A]
- **Bash same-line alias shorthand:** in a fresh shell without a pre-existing `m`, `shopt -s expand_aliases; alias m=id; m` does not use the newly defined alias on that input line. B23 deliberately forces a separate read through `eval`. [B-AL]
- **Bash builtin restriction:** `builtin id` does not execute the ordinary external utility. B30 invokes the actual builtin `eval`. [B-BI]
- **Bash here-document literal body:** a quoted delimiter disables outer-shell expansion. The body becomes code only if its recipient interprets it, as B09 does; the ordinary `:` recipient does not. [B-R] [B-B]
- **Bash substitution output:** punctuation produced by ordinary variable/command expansion is not automatically reparsed as shell control grammar. Explicit `eval`, another shell, arithmetic re-evaluation and the `${x@P}` prompt transformation are separate boundaries. [B-SC] [B-B] [B-SA] [B-PE]
- **PowerShell string literals:** `'whoami'`, `"whoami"`, and `'$(whoami)'` output strings without invoking the marker. Expandable `"$(whoami)"` is different. [P-Q] [P-IEX]
- **PowerShell uninvoked script block:** `{ whoami }` creates a block value; invocation requires a consumer such as `&`, `.Invoke()` or a callback parameter. [P-SB] [P-SBINV]
- **PowerShell call operator does not split a full command string:** `& 'whoami /user'` requests a command with that entire name; it does not run `whoami` with `/user`. [P-OP]
- **PowerShell comments:** `# whoami` and `<# whoami #>` do not execute the marker. Comment-like characters inside string/non-comment tokens are not comments; block comments do not concatenate fragments into a command name. `#Requires` is a directive with separate semantics, not an ordinary inert comment; module imports are covered by P55/P56. [P-COM] [P-REQ]
- **PowerShell single-quoted here-string:** its body is literal; P47 executes it only because `Invoke-Expression` consumes it as code. A bare single-quoted here-string containing `whoami` is not execution. [P-Q] [P-IEX]
- **PowerShell splatting alone:** a splat supplies arguments; it does not inherently execute a string. P14 becomes executable because the receiving parameter belongs to `Invoke-Expression`. [P-SPL] [P-IEX]
- **PowerShell stop-parsing:** `--%` is not itself an execution operator. It changes what a native recipient receives; that recipient may then execute its own language, as cmd.exe does in P24. [P-PARSE] [CMD]
- **PowerShell regex/wildcard text:** a string containing `whoami` used as a literal match pattern is data. Script-block predicates, subexpressions or code-generating replacements are separate execution contexts and must not be conflated with the regex syntax. [P-OP] [P-SW]



[B-CS]: https://www.gnu.org/software/bash/manual/html_node/Command-Substitution.html
[B-PS]: https://www.gnu.org/software/bash/manual/html_node/Process-Substitution.html
[B-R]: https://www.gnu.org/software/bash/manual/html_node/Redirections.html
[B-SA]: https://www.gnu.org/software/bash/manual/html_node/Shell-Arithmetic.html
[B-AE]: https://www.gnu.org/software/bash/manual/html_node/Arithmetic-Expansion.html
[B-A]: https://www.gnu.org/software/bash/manual/html_node/Arrays.html
[B-C]: https://www.gnu.org/software/bash/manual/html_node/Conditional-Constructs.html
[B-F]: https://www.gnu.org/software/bash/manual/html_node/Shell-Functions.html
[B-AL]: https://www.gnu.org/software/bash/manual/html_node/Aliases.html
[B-B]: https://www.gnu.org/software/bash/manual/html_node/Bourne-Shell-Builtins.html
[B-BI]: https://www.gnu.org/software/bash/manual/html_node/Bash-Builtins.html
[B-P]: https://www.gnu.org/software/bash/manual/html_node/Pipelines.html
[B-G]: https://www.gnu.org/software/bash/manual/html_node/Command-Grouping.html
[B-L]: https://www.gnu.org/software/bash/manual/html_node/Looping-Constructs.html
[B-CO]: https://www.gnu.org/software/bash/manual/html_node/Coprocesses.html
[B-BR]: https://www.gnu.org/software/bash/manual/html_node/Brace-Expansion.html
[B-T]: https://www.gnu.org/software/bash/manual/html_node/Tilde-Expansion.html
[B-PE]: https://www.gnu.org/software/bash/manual/html_node/Shell-Parameter-Expansion.html
[B-SC]: https://www.gnu.org/software/bash/manual/html_node/Simple-Command-Expansion.html
[B-Q]: https://www.gnu.org/software/bash/manual/html_node/Quoting.html
[B-E]: https://www.gnu.org/software/bash/manual/html_node/Escape-Character.html
[B-AC]: https://www.gnu.org/software/bash/manual/html_node/ANSI_002dC-Quoting.html
[B-LC]: https://www.gnu.org/software/bash/manual/html_node/Locale-Translation.html
[B-CM]: https://www.gnu.org/software/bash/manual/html_node/Comments.html
[B-FN]: https://www.gnu.org/software/bash/manual/html_node/Filename-Expansion.html
[B-V]: https://www.gnu.org/software/bash/manual/html_node/Bash-Variables.html
[B-ST]: https://www.gnu.org/software/bash/manual/html_node/Bash-Startup-Files.html
[B-CE]: https://www.gnu.org/software/bash/manual/html_node/Command-Search-and-Execution.html
[B-PC]: https://www.gnu.org/software/bash/manual/html_node/Programmable-Completion-Builtins.html
[B-H]: https://www.gnu.org/software/bash/manual/html_node/History-Interaction.html
[B-IN]: https://www.gnu.org/software/bash/manual/html_node/Invoking-Bash.html
[B-PR]: https://www.gnu.org/software/bash/manual/html_node/Controlling-the-Prompt.html
[B-HB]: https://www.gnu.org/software/bash/manual/html_node/Bash-History-Builtins.html
[B53]: https://lists.gnu.org/archive/html/bug-bash/2025-07/msg00005.html
[POSIX24]: https://pubs.opengroup.org/onlinepubs/9799919799/utilities/V3_chap02.html
[POSIX18]: https://pubs.opengroup.org/onlinepubs/9699919799/utilities/V3_chap02.html
[ENV]: https://www.gnu.org/software/coreutils/manual/html_node/env-invocation.html
[NOHUP]: https://www.gnu.org/software/coreutils/manual/html_node/nohup-invocation.html
[TIMEOUT]: https://www.gnu.org/software/coreutils/manual/html_node/timeout-invocation.html
[NICE]: https://www.gnu.org/software/coreutils/manual/html_node/nice-invocation.html
[XARGS]: https://www.gnu.org/software/findutils/manual/html_node/find_html/Invoking-xargs.html
[FIND]: https://www.gnu.org/software/findutils/manual/html_node/find_html/Single-File.html
[FIND+]: https://www.gnu.org/software/findutils/manual/html_node/find_html/Multiple-Files.html
[SUDO]: https://github.com/sudo-project/sudo/blob/main/docs/sudo.man.in
[P-OP]: https://learn.microsoft.com/en-us/powershell/module/microsoft.powershell.core/about/about_operators?view=powershell-5.1
[P-Q]: https://learn.microsoft.com/en-us/powershell/module/microsoft.powershell.core/about/about_quoting_rules?view=powershell-5.1
[P-SPL]: https://learn.microsoft.com/en-us/powershell/module/microsoft.powershell.core/about/about_splatting?view=powershell-5.1
[P-F]: https://learn.microsoft.com/en-us/powershell/module/microsoft.powershell.core/about/about_functions?view=powershell-5.1
[P-PARSE]: https://learn.microsoft.com/en-us/powershell/module/microsoft.powershell.core/about/about_parsing?view=powershell-5.1
[P-TR]: https://learn.microsoft.com/en-us/powershell/module/microsoft.powershell.core/about/about_trap?view=powershell-5.1
[P-SW]: https://learn.microsoft.com/en-us/powershell/module/microsoft.powershell.core/about/about_switch?view=powershell-5.1
[P-TRY]: https://learn.microsoft.com/en-us/powershell/module/microsoft.powershell.core/about/about_try_catch_finally?view=powershell-5.1
[P-PRE]: https://learn.microsoft.com/en-us/powershell/module/microsoft.powershell.core/about/about_command_precedence?view=powershell-5.1
[P-SCOPE]: https://learn.microsoft.com/en-us/powershell/module/microsoft.powershell.core/about/about_scopes?view=powershell-5.1
[P-PS]: https://learn.microsoft.com/en-us/powershell/module/microsoft.powershell.core/about/about_powershell_exe?view=powershell-5.1
[P-USING]: https://learn.microsoft.com/en-us/powershell/module/microsoft.powershell.core/about/about_using?view=powershell-5.1
[P-CALC]: https://learn.microsoft.com/en-us/powershell/module/microsoft.powershell.core/about/about_calculated_properties?view=powershell-5.1
[P-SB]: https://learn.microsoft.com/en-us/powershell/module/microsoft.powershell.core/about/about_script_blocks?view=powershell-7.5
[P-COM]: https://learn.microsoft.com/en-us/powershell/module/microsoft.powershell.core/about/about_comments?view=powershell-7.5
[P-SP]: https://learn.microsoft.com/en-us/powershell/module/microsoft.powershell.core/about/about_special_characters?view=powershell-7.5
[P-CHAIN]: https://learn.microsoft.com/en-us/powershell/module/microsoft.powershell.core/about/about_pipeline_chain_operators?view=powershell-7.5
[P-PWSH]: https://learn.microsoft.com/en-us/powershell/module/microsoft.powershell.core/about/about_pwsh?view=powershell-7.5
[P-CLASS]: https://learn.microsoft.com/en-us/powershell/module/microsoft.powershell.core/about/about_classes_constructors?view=powershell-7.5
[P-PARSE7]: https://learn.microsoft.com/en-us/powershell/module/microsoft.powershell.core/about/about_parsing?view=powershell-7.5
[P-REQ]: https://learn.microsoft.com/en-us/powershell/module/microsoft.powershell.core/about/about_requires?view=powershell-7.5
[P-COMP]: https://learn.microsoft.com/en-us/powershell/module/microsoft.powershell.core/about/about_comparison_operators?view=powershell-7.5
[P-IEX]: https://learn.microsoft.com/en-us/powershell/module/microsoft.powershell.utility/invoke-expression?view=powershell-5.1
[P-AL]: https://learn.microsoft.com/en-us/powershell/module/microsoft.powershell.utility/set-alias?view=powershell-5.1
[P-START]: https://learn.microsoft.com/en-us/powershell/module/microsoft.powershell.management/start-process?view=powershell-5.1
[P-FOR]: https://learn.microsoft.com/en-us/powershell/module/microsoft.powershell.core/foreach-object?view=powershell-7.5
[P-INV]: https://learn.microsoft.com/en-us/powershell/module/microsoft.powershell.core/invoke-command?view=powershell-5.1
[P-JOB]: https://learn.microsoft.com/en-us/powershell/module/microsoft.powershell.core/start-job?view=powershell-5.1
[P-MEM]: https://learn.microsoft.com/en-us/powershell/module/microsoft.powershell.utility/add-member?view=powershell-5.1
[P-MOD]: https://learn.microsoft.com/en-us/powershell/module/microsoft.powershell.core/new-module?view=powershell-5.1
[P-LEX]: https://learn.microsoft.com/en-us/powershell/scripting/lang-spec/chapter-02?view=powershell-5.1
[P-GRAM]: https://learn.microsoft.com/en-us/powershell/scripting/lang-spec/chapter-15
[P-CREATE]: https://learn.microsoft.com/en-us/dotnet/api/system.management.automation.scriptblock.create?view=powershellsdk-7.4.0
[P-SBINV]: https://learn.microsoft.com/en-us/dotnet/api/system.management.automation.scriptblock.invoke?view=powershellsdk-7.4.0
[P-ADD]: https://learn.microsoft.com/en-us/dotnet/api/system.management.automation.powershell.addscript?view=powershellsdk-7.4.0
[P-PROC]: https://learn.microsoft.com/en-us/dotnet/api/system.diagnostics.process.start
[P-VALID]: https://learn.microsoft.com/en-us/powershell/scripting/developer/cmdlet/validatescript-attribute-declaration
[CMD]: https://learn.microsoft.com/en-us/windows-server/administration/windows-commands/cmd

## Open facts

The following platform/installation facts remain **unverified on real Windows**. Documentation establishes the grammar; the experiments settle the exact interpreter build, transport, installed utility or ambient state used by unreagent.

1. **Windows PowerShell 5.1 versus installed PowerShell 7:** run every P-series specimen through each executable with `-NoProfile`; separately call `[System.Management.Automation.Language.Parser]::ParseInput` on the original text. Record parse errors, command/string AST extents, marker output and spawned processes. In particular, P21/P34/P36 must distinguish unsupported 5.1 forms from supported 7 forms; P29 must reconstruct `whoami`, while replacing backtick-h with backtick-a must not. [P-LEX] [P-SP] [P-CHAIN] [P-FOR] Also compare P55 as an invoked `.ps1` file against pasting its `#Requires` body into `-Command`; record whether requirements are enforced and module initialization runs in each context. [P-REQ]
2. **Exact native transport through unreagent:** submit P23–P28 and the multiline P30/P46/P47 through the launcher's real shell invocation, with LF and separately CRLF input. Record the actual child command line using Process Monitor and compare the interpreter's received source with the fixture bytes. Run the encoded specimen in both executables and verify exactly one `whoami` execution. Native argument handling changed in PowerShell 7.3, so also record `$PSNativeCommandArgumentPassing` where present. [P-PARSE7] [P-PS] [P-PWSH]
3. **Windows Bash distribution and patch level:** run B03/B05/B06/B08–B18/B23/B57 under each supported Git Bash, MSYS2 or WSL Bash, recording `BASH_VERSION`, exit status, stderr and marker execution. Run B04 specifically under 5.2 and 5.3 and observe rejection versus execution. Process substitution depends on the port's FIFO or descriptor-path support. [B-PS] [B53]
4. **Installed wrapper semantics:** run B32–B40 with the actual `env`, `xargs`, `find`, `nohup`, `timeout`, `sudo` and `nice` binaries used on Windows. Record executable paths and versions first, then each marker child and argv. Do not infer GNU option semantics from the filename, particularly `find` or `timeout`; unsupported/absent wrappers are refusals, not successful corpus cases. [ENV] [XARGS] [FIND] [TIMEOUT] [SUDO]
5. **Lookup/session-state boundary:** run B25/B26/B48/B60/B62/B65 and P05/P35 with the explicitly supplied fixture state, then with that state absent or changed. Confirm that the command-line-only checker refuses both before execution. Separately test profiles/imported aliases/functions and executable search paths used by unreagent; record actual resolution rather than only marker spelling. [B-ST] [B-CE] [P-PRE]
6. **Deferred execution observability:** run B44–B46/B61/B63/B64 and P20/P38/P43 while recording child creation and output until the hosting shell/job actually finishes. Confirm the checker exposes the marker before the callback/event, and that the test harness does not declare success merely because the immediate foreground command returned. [B-CO] [B-B] [B-V] [B-BI] [P-JOB] [P-TR] [P-MEM]
