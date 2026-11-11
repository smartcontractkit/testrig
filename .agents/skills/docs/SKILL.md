---
name: docs
description: Write or update good-vibes living docs in docs/. Use when a change touches behavior, and when the user asks for documentation.
---

# Write living docs

`docs/` describes the system as it is now, not its history. A change updates the docs it invalidates.

## Doc types

The folders are configured in `good-vibes.toml`. The default split follows Diátaxis:

- `concepts/` — how a part of the system works, and why
- `guides/` — task-oriented steps for a person using the system
- `runbooks/` — operating procedures, including rollback
- `reference/` — exact behavior: flags, endpoints, schemas
- `decisions/` — one ADR per significant decision, written by `good-vibes done`

## Steps

1. Find the doc the change invalidates. Start with `good-vibes docs list`, then `rg` over `docs/`.
2. Update it in the same change as the code. Read it with `good-vibes docs get <id>#<heading>` when you only need one
   section.
3. If no doc covers new behavior, add one in the right folder. Start from the type's template:

   ```sh
   good-vibes docs template reference --id config --title "Configuration"
   ```

4. Fill in the front matter. `summary` is one sentence, `sources` lists the code globs that justify the doc, and
   `last_verified` is today.
5. Run `good-vibes docs lint`, then `good-vibes docs index` so `llms.txt` matches.

## Rules

- Every `concepts/` doc keeps a high-level Mermaid diagram in its Overview: the parts and how they relate. Add more in
  How it works when the flow needs them. Update the diagram in the same change as the behavior. Other doc types use
  diagrams only where they clarify.
- Update the doc in place. Do not append a changelog; history lives in git.
- Short paragraphs, one idea each. Code samples must run.
- If a doc describes behavior that no longer exists, delete or rewrite it. A stale doc is worse than no doc.
- `good-vibes docs stale` only warns. A warning means re-read the doc and either update it or bump `last_verified`.
