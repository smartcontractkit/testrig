---
name: implement
description: Implement one phase of a good-vibes change, following changes/<slug>/design.md. Use when the user asks to implement a change or continue a phase.
---

# Implement a change

## Steps

1. Read `changes/<slug>/proposal.md` and `design.md`. Read the newest handoff in `changes/<slug>/handoffs/` when one
   exists.
2. Work through the current phase in order, one task at a time.
3. After each task, build and run the project's tests. Tick the task in `design.md` (`- [ ]` to `- [x]`) only after it
   works.
4. Do any `Docs:` task in the phase. If the doc the change needs does not exist, write it.
5. At the end of the phase, write `changes/<slug>/handoffs/phase-N.md` with:
   - **Done** — what landed
   - **Deviations** — where the design was wrong, and what you did instead
   - **Open issues** — anything the next phase must know
   - **Files touched**
   - **Next-phase notes**
6. Run `good-vibes check <slug>`.
7. After the last phase, close the change with the `close` skill.

## Rules

- In an orchestrated run (`good-vibes implement --style orchestrated`), the binary ticks boxes, commits, and runs
  verify. Follow the prompt you are given; do not tick, commit, or run `good-vibes done`.
- Do not reword an open task. If the design is wrong, update `design.md` first, then implement.
- Do not commit or push unless the user asks.
- Keep the tree green after every task. A half-done task is not ticked.
- If the phase needs a doc the design did not name, add the task to `design.md` in the same change.
