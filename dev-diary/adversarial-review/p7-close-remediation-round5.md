# P7 close remediation, round 5

I did not change the round 5 verdict. I fixed finding M1 and nothing else.

I edited `web/src/lib/progress.ts` and this file. I did not commit.

| Finding | Fix | Pin | Residue |
|---|---|---|---|
| M1, the reconnect timer types as `number` and the clean-clone Svelte check fails | `web/src/lib/progress.ts:76` now declares `let timer: ReturnType<typeof setTimeout> \| undefined`. The declaration adapts to whichever `setTimeout` the installed types resolve, so the browser `number` and the Node `Timeout` both satisfy it. I changed no reconnect logic, no backoff constant, and no other line. | In a clean clone at `c4f1f94` after `npm ci`, `PATH=/opt/homebrew/opt/node/bin:$PATH npm --prefix web run check` exits 0 and reports zero errors and zero warnings. The same check in the source worktree also exits 0. | None. |

## Acceptance 1: the clean clone reproduces the failure before the fix

I cloned the repository to `/tmp/p7-rem-r5` at `c4f1f94` and installed from the committed lockfile with `npm --prefix web ci`.

```text
$ npm --prefix web run check
/private/tmp/p7-rem-r5/web/src/lib/progress.ts:106:5
Error: Type 'Timeout' is not assignable to type 'number'.
====================================
svelte-check found 1 error and 0 warnings in 1 file
before-fix exit=1
```

The finding measures correctly. The clean clone resolves `setTimeout` through the Node types, so the assignment at line 106 rejects.

## Acceptance 2: the fix clears the clean-clone check

I applied the same one-line change inside the clone and reran the check against the same `node_modules`.

```text
$ npm --prefix web run check
Getting Svelte diagnostics...
svelte-check found 0 errors and 0 warnings
after-fix exit=0
```

Restoring `number` restores the failure, which matches the reviewer's mutation.

## Acceptance 3: the source worktree check

```text
$ PATH=/opt/homebrew/opt/node/bin:$PATH npm --prefix web run check
Loading svelte-check in workspace: /Users/narayan/Documents/work/ajilamu/web
Getting Svelte diagnostics...
svelte-check found 0 errors and 0 warnings
```

`npm --prefix web run build` also passed and wrote `web/build` through the static adapter.

## Acceptance 4: reconnect behaviour did not change

A type annotation should erase at emit, so I measured the emitted JavaScript rather than trusting that claim.
I ran the repository's own TypeScript on `progress.ts` in both states and hashed the output.

```text
c88ca7c8b390ded92fc59fd3ec345e65afcfaf0a623cdd943b8e181df4491da7  /tmp/emit-before/progress.js
c88ca7c8b390ded92fc59fd3ec345e65afcfaf0a623cdd943b8e181df4491da7  /tmp/emit-after/progress.js
EMITTED JAVASCRIPT IDENTICAL
```

`diff` reported no difference. The backoff schedule, the six-attempt ceiling, the `clearTimeout` in `stop`, and every phase sentence stay byte for byte identical.

I also compared the bundled Vite chunk across the two states, and the hashes differed.
That comparison proves nothing, because two builds of unchanged source also produced different chunk hashes.
The `tsc` emit above carries the behaviour evidence instead.

## Diff

```diff
-  let timer: number | undefined
+  let timer: ReturnType<typeof setTimeout> | undefined
```

`git diff --stat` reports one file changed, one insertion, and one deletion.

I did not tick any PHASE-7 exit box. I did not edit the round 5 review file, the README, any phase file, or the workspace page.
