---
name: propose
description: Write a good-vibes proposal in changes/<slug>/proposal.md. Use when the user wants to propose a change, start a new feature, or asks for a proposal.
---

# Propose a change

A proposal explains why a change is worth making and sets the direction. It includes a brief high-level how and why this
way, saving detailed mechanics for the design.

## Steps

1. Choose a slug: lowercase, hyphenated, no number prefix. Run `good-vibes propose <slug> --title "<Title>"`. This
   writes `changes/<slug>/proposal.md`.
2. Set `effort` in the front matter to `easy`, `medium`, or `hard`. Rate the model reasoning depth and context breadth
   the change needs, not human hours or diff size; `[implement.effort]` maps the rating to worker and reviewer models.
   Leave the key out to skip the rating.
   - `easy` — narrow, pattern-following context. A local, well-specified change that follows an existing pattern in a
     few files, with simple verification and no subtle state or cross-package invariants. A fast or mid-tier model
     implements and reviews it reliably.
   - `medium` — multi-seam context and moderate design judgment. Coordinates across several packages or interfaces, adds
     a seam or state transition, or weighs trade-offs while holding multiple modules in context. A capable model tracks
     contracts across boundaries.
   - `hard` — deep reasoning and broad or subtle context. Ambiguous requirements, concurrency or state machines,
     security or git/worktree invariants, or architectural redesign where a surface fix hides regressions. A frontier
     reasoning model implements and reviews it.
3. Fill the three sections in plain language:
   - **Context** — what happens today and why it hurts. Use numbers or links when you have them. Show today's shape with
     a Mermaid diagram when a picture is clearer than prose.
   - **Goals and non-goals** — why solve it now, what success looks like, and explicit non-goals to bound scope.
   - **Decision** — a few sentences of text, and let diagrams carry the shape. Lead with a Mermaid **Before** and
     **After** pair that shows what the change moves, adds, or removes. Say why it beats obvious alternatives, and why
     the `effort` rating fits, in one sentence each. For `effort`, name the reasoning depth and context breadth the
     change needs, not file count, line count, or human hours.
4. Delete the HTML comments once a section is done.
5. Leave `status: proposed`. A person accepts the change, not an agent.
6. Run `good-vibes check <slug>` and fix what it reports.

## Rules

- Prefer Mermaid diagrams to prose for structure, flow, and ownership. Keep each one high-level: boxes for parts, arrows
  for who calls or owns what. Mark what changed (a new node, a removed edge) so the reader sees it at a glance. Do not
  draw file-level detail; that belongs in the design.
- Save detailed mechanics for the design: no file paths, schemas, or API signatures. The proposal gives the high-level
  shape and rationale; `design.md` specifies how it works.
- Do not accept your own proposal. `good-vibes accept` needs a person's name; a review URL is optional.
- One change, one problem. If you need two Decisions, write two proposals.
