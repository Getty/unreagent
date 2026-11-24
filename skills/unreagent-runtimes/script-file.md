# unreagent host runtimes: the script file in the project

Which directory the script is written to is in the skill itself, "Where the
script lands".

Two side effects follow from the script being a real file in the project:

- **It is briefly visible there.** Anything watching that directory — UE's
  directory watcher, a source-control client, a bundler in watch mode — can
  observe an `unreagent-<digits>.py` / `.mjs` appear and vanish within one run.
- **A hard kill leaves it behind.** The deletion is a deferred call in the
  launcher; killing the launcher skips it. At the next start the launcher sweeps
  the runtime directories and logs one line per file it finds:

  ```
  Stale script file removed: <path>
  WARN could not remove stale script file <path>: <error>
  ```

  The sweep matches only the generated shape — the prefix, then decimal digits,
  then `.py` or `.mjs`. A hand-written `unreagent-helper.py` is deliberately
  safe from it.

Because a leftover script sits in the user's repository, the UE project's own
`.gitignore` should carry:

```gitignore
unreagent-[0-9]*.py
unreagent-[0-9]*.mjs
```

The launcher's own repository has these entries, which does nothing for the
project the launcher is running in. If you are set up in a project that lacks
them, adding them is a reasonable thing to offer.
