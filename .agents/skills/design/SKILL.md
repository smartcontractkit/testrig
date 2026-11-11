---
name: design
description: Write a good-vibes design in changes/<slug>/design.md, including the TODO phases. Use when a proposal is accepted and the user wants to plan the implementation.
---

# Design a change

The design says how the change works. The TODO section says what to do, in order.

## Steps

1. Run `good-vibes design <slug>` to scaffold `changes/<slug>/design.md`.
2. Fill the sections:
   - **Context** — technical codebase context: packages, seams, invariants, and constraints. Do not repeat the
     proposal's pitch. Diagram the current package or data layout when it helps.
   - **Approach** — module seams, interfaces/types, data flow, and technical trade-offs or rejected options. Map how the
     change works with detailed Mermaid diagrams, more detailed than the proposal's.
   - **Verification** — complete end-to-end checks, integration scenarios, edge cases, and behavioral verification.
     Collapse standard test/lint commands into one line.
   - **Risks** — what could break, failure modes, and how we would notice.
3. Write the TODO:
   - One `### Phase N: Name` per step that leaves the repo green.
   - Give each phase 1–3 vertical tasks. Each task ships a working slice of behavior with its tests, so code and tests
     land together.
   - Do not make standalone "add tests" tasks. Do not split one feature into horizontal layers like types, helpers,
     wiring, and tests.
   - Put doc updates in the task that changes the behavior. When docs span the whole phase, add one `Docs:` task at the
     end. Do not add `Docs: none` placeholders.
   - Rate the design with an `Effort: <easy|medium|hard> (<reason>)` line under `## TODO`, before the first phase. Rate
     the model reasoning depth and context breadth the work needs, not human hours or diff size:
     - `easy` — narrow, pattern-following context. A local, well-specified change that follows an existing pattern in
       one or a few files, with simple verification and no subtle state or cross-package invariants.
     - `medium` — multi-seam context and moderate design judgment. Coordinates across several packages or interfaces,
       adds a seam or state transition, or weighs trade-offs while holding multiple modules in context.
     - `hard` — deep reasoning and broad or subtle context. Ambiguous requirements, concurrency or state machines,
       security or git/worktree invariants, or architectural redesign where a surface fix hides regressions.
   - Give a phase its own `Effort:` line under its heading whenever its cognitive load or context scope differs from the
     design, such as an `easy` mechanical wiring phase inside a `medium` design or a `hard` concurrency phase inside a
     `medium` design. Phrase every `<reason>` in terms of reasoning and context demands.
4. Run `good-vibes design <slug>` again. It validates the TODO grammar and moves the status to `designed`.
5. Run `good-vibes check <slug>`.

## Rules

- Use the Mermaid type that fits: `flowchart` for data flow and module seams, `sequenceDiagram` for calls between parts,
  `stateDiagram-v2` for lifecycles, `classDiagram` or `erDiagram` for types and schemas. Draw every new seam, state, and
  failure path. Add one diagram per idea, not one giant diagram.
- A diagram is a sketch like any code in the design: keep it small enough to stay true.
- Phases run in sequence unless they share a parallel group.
- To mark a group, put a `Parallel: <name>` line directly under each phase's heading, before its tasks, using the same
  name for each phase:

  ```markdown
  ### Phase 3: Add the export command

  Parallel: surfaces

  - [ ] ...
  ```

- Group only phases that touch separate areas of the code and depend on the work before the group, not on each other.
  Land the shared foundation in an earlier phase. The merge and combined verify are the real proof.
- A group needs at least two phases, its phases must be next to each other, and its name may mark only one group.
  `good-vibes check` rejects a malformed group.
- Do not tick checkboxes. The implement flow owns them.
- Tasks may wrap. The Markdown formatter reflows long lines, and the TODO parser reads indented continuation lines as
  part of the task.
- Code in the design is a sketch. Keep it short enough to stay true.
