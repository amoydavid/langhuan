# File Browser Dialog Refactor Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the file-content three-column workspace with a compact two-pane folder browser whose file inspection and short mutations are dialogs.

**Architecture:** The file tree becomes a folders-only navigation control, with the selected folder expressed in route search state. A new right-pane file list derives direct file children from the File Tree response and opens the existing document route as a modal overlay. Upload, create-folder, and rename use shadcn Dialogs while keeping the current TanStack Query mutations and cache invalidation.

**Tech Stack:** React 19, TypeScript, TanStack Router/Query, React Hook Form, Zod, shadcn/Radix UI, Vitest browser tests.

## Global Constraints

- Reuse the existing AppShell, semantic Tailwind tokens, shadcn/Radix primitives, and shared Axios client.
- Do not render UUIDs, hashes, raw payloads, or untrusted HTML in the normal UI.
- Preserve real backend statuses, workspace scope, existing permissions, and cache invalidation.
- Preserve direct links, refresh, and browser history through typed route search parameters.
- Keep desktop panes independently scrollable; use semantic Dialog behavior and keyboard-accessible tree/menu controls.

---

### Task 1: Establish folders-only navigation behavior

**Files:**
- Modify: `web/src/features/content/file-tree/file-tree-model.ts`
- Modify: `web/src/features/content/file-tree/file-tree.tsx`
- Test: `web/src/features/content/file-tree/file-tree.test.tsx`

- [ ] Write failing browser tests showing the root and folders but no file node, and asserting that selecting a folder calls `onSelectFolder`.
- [ ] Run `pnpm --dir web test -- file-tree.test.tsx` and confirm the test fails because file tree items are still rendered.
- [ ] Make folder filtering explicit in the tree model, include the root as a tree item, and remove document selection from `FileTree`.
- [ ] Replace inline create/rename forms with dialog triggers while retaining accessible keyboard tree navigation and folder action menus.
- [ ] Re-run the focused test and confirm it passes.

### Task 2: Build the selected-folder file list and upload dialog

**Files:**
- Create: `web/src/features/content/file-tree/file-browser-list.tsx`
- Create: `web/src/features/content/file-tree/upload-file-dialog.tsx`
- Modify: `web/src/features/content/file-tree/file-tree-workspace.tsx`
- Modify: `web/src/lib/i18n/locales/zh/content.ts`
- Modify: `web/src/lib/i18n/locales/en/content.ts`
- Test: `web/src/features/content/file-tree/file-browser-list.test.tsx`

- [ ] Write failing browser tests for a right pane that lists only direct file children of the selected folder and opens a selected file through its callback.
- [ ] Run the focused test and confirm it fails because the file list component does not exist.
- [ ] Implement the list with current-folder breadcrumb, file search/status/sort controls, compact status badges, a row menu, and an empty state.
- [ ] Implement the reusable upload Dialog around `DocumentUploadForm`; after success, close it and invalidate the same file/tree/summary queries.
- [ ] Connect root/folder selection and both upload triggers in `FileTreeWorkspace`.
- [ ] Re-run focused tests and confirm they pass.

### Task 3: Turn file inspection into a four-tab modal

**Files:**
- Modify: `web/src/routes/_authenticated/workspaces/$workspaceSlug/kb/$kbId/content/files/$documentId.tsx`
- Modify: `web/src/features/content/document-preview/document-preview.tsx`
- Test: `web/src/features/content/document-preview/document-preview.test.tsx`
- Test: `web/src/features/content/document-preview/file-detail-route.test.ts`

- [ ] Write failing tests for an accessible file-detail Dialog with Preview, Raw Markdown, Chunks, and File Information tabs.
- [ ] Run focused tests and confirm they fail because the document route still renders a main-pane layout.
- [ ] Render the detail route as a Dialog overlay, move the chunk inspector into its fourth tab, and retain chunk revision/conflict behavior.
- [ ] On close, navigate back to the files route while preserving selected-folder search state and restoring focus to the list row.
- [ ] Re-run focused tests and confirm they pass.

### Task 4: Compact shell and end-to-end verification

**Files:**
- Modify as needed: `web/src/features/knowledge-bases/workbench/workbench-layout.tsx`
- Test: `web/src/features/knowledge-bases/workbench/workbench-layout.test.tsx`

- [ ] Confirm the workbench header is compact when a full-height content child is active and that its primary action opens the upload Dialog from the File view.
- [ ] Run targeted browser tests, then `pnpm --dir web check`, `pnpm --dir web test`, `pnpm --dir web build`, and `git diff --check`.
- [ ] Review each user requirement against the rendered-component tests and current diff before reporting completion.
