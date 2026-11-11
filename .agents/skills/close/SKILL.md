---
name: close
description: Write a good-vibes close-out in changes/<slug>/closeout.md before the change closes. Use when every phase of a change is done and the user wants to close it.
---

# Close a change

`good-vibes done` turns the proposal and the close-out into the ADR, then deletes the change folder. The ADR is the only
record the change leaves, so the close-out keeps what a future reader needs and drops the rest.

## Steps

1. Read `changes/<slug>/proposal.md`, `design.md`, `summary.md` when the change ran, and every handoff in
   `changes/<slug>/handoffs/`.
2. Write `changes/<slug>/closeout.md` with a `# Close-out` title and these sections, in order:
   - **Consequences** — what got easier, what got harder, and what we would revisit.
   - **Alternatives considered** — rejected options and trade-offs distilled from `proposal.md`, `design.md`, and
     `handoffs/`, explaining why each was not chosen.
   - **Implementation notes** — where the plan was wrong, and limits a user or maintainer will hit.
   - **Follow-ups** — work left open, each item specific enough to propose.
3. Write "None." in a section with nothing to say. `done` refuses an empty or missing section.
4. Ask a person to review the close-out. Once they approve, run `good-vibes done <slug>`.

## Rules

- Distill; do not copy. Phase-by-phase logs, file lists, and task names stay out.
- Keep a fact only if it is still true and not already in `docs/`. If it describes how the system works now, update the
  doc instead.
- Do not run `good-vibes done` before a person has reviewed the close-out. It deletes the working papers.
