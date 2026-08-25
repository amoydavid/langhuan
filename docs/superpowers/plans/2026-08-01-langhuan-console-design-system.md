# Langhuan Console Design System Skill Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Create a project-local `langhuan-console-design-system` skill that guides planning, implementation, and review of the entire Langhuan Web Console while treating the current React code and approved product specifications as the source of truth.

**Architecture:** Use a hybrid skill: a concise `SKILL.md` owns triggers, workflow, source priority, and non-negotiable rules; four one-level reference files hold stable visual, composition, product-experience, accessibility, and verification guidance. Do not copy CSS, React components, screenshots, or GetRank artifacts into the skill; agents inspect the live repository before changing the console.

**Tech Stack:** Codex skill format, Markdown, `agents/openai.yaml`, Python skill-creator validation, React 19, TypeScript 7, Tailwind CSS v4, shadcn/ui, Radix UI, TanStack Router/Query, React Hook Form, Zod, Lucide React, Biome, Vitest.

## Global Constraints

- Create the skill at `.codex/skills/langhuan-console-design-system/`.
- Keep `.codex/` local and ignored; do not force-add skill files while the user's `.gitignore` change ignores that directory.
- Do not modify or overwrite the user's existing `.gitignore` change.
- Treat `AGENTS.md`, approved feature specifications, live API behavior, and current `web/` code as higher priority than copied design guidance.
- Preserve the existing engineering-green tokens, light/dark/system theme contract, Geist/Inter/JetBrains Mono roles, Lucide icons, shadcn/Radix primitives, and product brand assets.
- Preserve compact visual controls while guaranteeing at least `44×44px` mobile interaction targets through layout or hit-area expansion.
- Never invent progress, processing stages, KPI values, permissions, resource relationships, or retry actions absent from the backend contract.
- Do not create a README, changelog, CSS snapshot, component template, font copy, icon copy, screenshot, or duplicate implementation source.
- Use `apply_patch` for hand-authored file edits after initialization.

---

## File Map

- Create: `.codex/skills/langhuan-console-design-system/SKILL.md` — trigger metadata, source priority, workflow, hard rules, and reference routing.
- Create: `.codex/skills/langhuan-console-design-system/agents/openai.yaml` — UI metadata and default invocation prompt.
- Create: `.codex/skills/langhuan-console-design-system/references/foundations.md` — source anchors and stable visual foundations.
- Create: `.codex/skills/langhuan-console-design-system/references/application-patterns.md` — AppShell, composition, component reuse, forms, routing, and query patterns.
- Create: `.codex/skills/langhuan-console-design-system/references/product-experience.md` — task-centered IA, truthfulness, permissions, naming, async feedback, and safety.
- Create: `.codex/skills/langhuan-console-design-system/references/accessibility-and-verification.md` — responsive behavior, touch targets, keyboard/focus behavior, and verification.

### Task 1: Initialize the Project-Local Skill

**Files:**
- Create: `.codex/skills/langhuan-console-design-system/SKILL.md`
- Create: `.codex/skills/langhuan-console-design-system/agents/openai.yaml`
- Create: `.codex/skills/langhuan-console-design-system/references/`

**Interfaces:**
- Consumes: skill-creator `init_skill.py` and the approved target directory.
- Produces: a skill skeleton named `langhuan-console-design-system` with a references directory and UI metadata.

- [ ] **Step 1: Confirm the target does not already exist and preserve the dirty worktree**

Run:

```bash
test ! -e .codex/skills/langhuan-console-design-system
git status --short
```

Expected: the first command exits `0`; status still shows the user's `.gitignore` modification.

- [ ] **Step 2: Initialize the skill with deterministic interface metadata**

Run:

```bash
python3 ~/.codex/skills/.system/skill-creator/scripts/init_skill.py \
  langhuan-console-design-system \
  --path .codex/skills \
  --resources references \
  --interface 'display_name=琅嬛 Console 设计系统' \
  --interface 'short_description=按现有系统设计、实现与评审琅嬛 Web Console' \
  --interface 'default_prompt=使用 $langhuan-console-design-system 设计并实现符合现有系统的琅嬛 Web Console 页面。'
```

Expected: output reports creation of `SKILL.md`, `agents/openai.yaml`, and `references/`.

- [ ] **Step 3: Inspect the generated metadata**

Run:

```bash
sed -n '1,120p' .codex/skills/langhuan-console-design-system/agents/openai.yaml
find .codex/skills/langhuan-console-design-system -maxdepth 2 -print | sort
```

Expected: `openai.yaml` contains quoted `display_name`, `short_description`, and a `default_prompt` naming `$langhuan-console-design-system`; no asset or script directory exists.

### Task 2: Author the Core Skill Workflow

**Files:**
- Replace: `.codex/skills/langhuan-console-design-system/SKILL.md`

**Interfaces:**
- Consumes: `AGENTS.md`, current feature specifications, and the live paths named below.
- Produces: a concise workflow that routes detailed questions to four one-level references.

- [ ] **Step 1: Replace generated frontmatter and instructions**

Use `apply_patch` to write frontmatter containing only:

```yaml
---
name: langhuan-console-design-system
description: Design, implement, and review the Langhuan Web Console using its current engineering-green visual system and product contracts. Use for any Langhuan admin-console screen, AppShell or navigation change, workbench, dashboard, data table, form, status or async feedback, responsive layout, accessibility work, frontend experience specification, or UI code review in this repository.
---
```

Then write these body sections:

- `Establish the source of truth`: read `AGENTS.md`, the approved feature spec, `theme.css`, `index.css`, relevant layout/UI/feature code, API types, query options, loaders, permissions, and tests.
- `Work through the page in this order`: product truth → route truth → Query ownership → existing component composition → RHF/Zod forms → responsive task equivalence → accessibility → verification.
- `Non-negotiable rules`: semantic tokens, light/dark/system, Lucide, shared primitives, compact visuals plus `44×44px` touch targets, complete AppShell, backend-supported facts only, readable names, status text, mobile cards, URL ownership, no `any`, no component-level `fetch`, no unsafe HTML, and no decorative dashboards.
- `Load detailed guidance only when needed`: direct links to all four reference files.
- `Completion standard`: real loading/empty/forbidden/failed/processing/completed states; direct entry and refresh; desktop/mobile task equivalence; relevant checks pass.

- [ ] **Step 2: Check frontmatter and reference reachability**

Run:

```bash
sed -n '1,240p' .codex/skills/langhuan-console-design-system/SKILL.md
rg -n '^## |references/' .codex/skills/langhuan-console-design-system/SKILL.md
```

Expected: frontmatter contains only `name` and `description`; all four references are linked directly; generated scaffolding text is gone.

### Task 3: Author the Stable Reference Contracts

**Files:**
- Create: `.codex/skills/langhuan-console-design-system/references/foundations.md`
- Create: `.codex/skills/langhuan-console-design-system/references/application-patterns.md`
- Create: `.codex/skills/langhuan-console-design-system/references/product-experience.md`
- Create: `.codex/skills/langhuan-console-design-system/references/accessibility-and-verification.md`

**Interfaces:**
- Consumes: current `web/` code, the approved design spec, and the v0.5.0 experience spec as the first broad example.
- Produces: four focused references with no copied implementation assets.

- [ ] **Step 1: Write foundations.md**

Use `apply_patch` to add:

- Live sources: `web/src/styles/theme.css`, `web/src/styles/index.css`, `web/src/assets/logo.tsx`, `web/public/images/`, and their contract tests.
- Theme rules: engineering green as the restrained brand anchor; semantic Tailwind tokens; real semantic statuses; light/dark/system; update light, dark, Tailwind binding, and tests together when adding a semantic token.
- Typography roles: Geist Sans for UI, Inter for dense reading, JetBrains Mono for code/technical locators/aligned numeric data, never as a reason to expose UUIDs.
- Shape and density: current radii and component sizes, restrained borders/shadows, no decorative gradients or oversized hero treatment.
- Icons and assets: Lucide React and existing Logo/SVG assets only.

- [ ] **Step 2: Write application-patterns.md**

Use `apply_patch` to add:

- Live layout/UI/status/table/form/query/route source paths.
- Authenticated AppShell composition and mobile drawer behavior.
- Page header, primary task action, cards, real tables, mobile cards, `StatusBadge`, Skeleton, empty/error surfaces, Sheet, Dialog, and Toast selection.
- RHF + Zod forms; safe draft preservation; TanStack Router loaders and typed search params; TanStack Query and precise invalidation; shared axios client; sanitized Markdown.
- Search existing primitives before adding feature wrappers; avoid one-page changes to shared primitives.

- [ ] **Step 3: Write product-experience.md**

Use `apply_patch` to add:

- Task-centered information architecture and Workspace/KnowledgeBase context.
- URL as recoverable work state.
- Backend-owned permissions; 403 versus cross-Workspace 404.
- Readable names; no ordinary UUID/hash rendering; structured copy-diagnostics action.
- Real async states and polling; no invented progress, retry, statistics, or completion claims.
- Actionable empty/error copy and consequence-specific destructive confirmations.
- Typed metadata, safe Markdown, and the requirement to read the current approved feature spec for feature-specific contracts.

- [ ] **Step 4: Write accessibility-and-verification.md**

Use `apply_patch` to add:

- Full AppShell at every authenticated route.
- Desktop/tablet/mobile task equivalence; real tables become mobile cards; file work becomes a focused single-column flow.
- Techniques for `44×44px` mobile hit targets around compact controls.
- Skip link, Radix keyboard semantics, tree keys when relevant, focus entry/restoration, `aria-describedby`, status text plus tone, light/dark focus contrast, reduced motion, and no page-level horizontal overflow.
- Direct-entry/F5/back-forward checks, component/route/E2E expectations, exact frontend commands, and temporary pgvector/Redis requirements for data E2E.

- [ ] **Step 5: Check reference focus and source paths**

Run:

```bash
for file in .codex/skills/langhuan-console-design-system/references/*.md; do
  wc -l "$file"
  sed -n '1,24p' "$file"
done
test -f web/src/styles/theme.css
test -f web/src/styles/index.css
test -d web/src/components/ui
test -d web/src/components/layout
test -f docs/superpowers/specs/2026-08-01-v0.5.0-web-console-experience-design.md
```

Expected: all four files exist, each has one focused responsibility, and every named live source path resolves.

### Task 4: Validate and Forward-Test the Skill

**Files:**
- Inspect: `.codex/skills/langhuan-console-design-system/`
- Preserve: `.gitignore`

**Interfaces:**
- Consumes: the complete skill folder from Tasks 1–3.
- Produces: structural validation, content integrity evidence, and one realistic usage evaluation.

- [ ] **Step 1: Run the official structural validator**

Run:

```bash
python3 ~/.codex/skills/.system/skill-creator/scripts/quick_validate.py \
  .codex/skills/langhuan-console-design-system
```

Expected: `Skill is valid!`

- [ ] **Step 2: Scan for scaffolding residue and duplicate assets**

Run:

```bash
pattern="$(printf '%s%s|%s%s|%s%s|%s%s|%s%s' TO DO TB D FIX ME example _resource place holder)"
rg -n "$pattern" .codex/skills/langhuan-console-design-system || true
find .codex/skills/langhuan-console-design-system -maxdepth 2 -type d | sort
sed -n '1,120p' .codex/skills/langhuan-console-design-system/agents/openai.yaml
```

Expected: the scan prints nothing; only the root, `agents`, and `references` directories exist; metadata matches Task 1.

- [ ] **Step 3: Forward-test without editing production code**

Use a fresh agent context with:

```text
Use $langhuan-console-design-system at .codex/skills/langhuan-console-design-system to propose the implementation shape for a Langhuan knowledge-base retrieval results page. Do not edit files. Identify the live sources you would inspect, route and Query ownership, desktop/mobile composition, real-state rules, and accessibility checks.
```

Expected:

- It reads `AGENTS.md`, the relevant approved spec, live theme/layout/UI components, and feature/API/query code.
- It uses AppShell, semantic tokens, Lucide, TanStack Router/Query, a desktop result list/table, and a mobile task-equivalent layout.
- It does not copy GetRank HTML/CSS, invent KPI/progress data, expose ordinary UUIDs, or drop light/dark/system support.
- It handles direct entry/refresh, loading/empty/error/forbidden/processing states, keyboard focus, and `44×44px` touch hit areas.

- [ ] **Step 4: Verify the user's ignored-file change remains untouched**

Run:

```bash
git diff -- .gitignore
git status --short
```

Expected: `.gitignore` still contains only the user's prior `.codex/` ignore change relative to `HEAD`; the skill remains local and ignored; no unrelated tracked files changed during execution.

- [ ] **Step 5: Report completion without force-adding the skill**

Report the absolute skill path, created files, validator output, forward-test outcome, and the fact that `.codex/` remains intentionally ignored. Do not stage or commit ignored skill files.
