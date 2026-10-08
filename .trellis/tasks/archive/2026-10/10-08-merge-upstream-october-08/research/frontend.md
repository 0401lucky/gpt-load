# Frontend merge verification

## Resolution

- Resolved the three frontend conflicts in `modern/api/credential-actions.ts`, `modern/features/groups/CredentialDetailPanel.vue`, and `modern/i18n/locales/credential-cards.ts`.
- `name` and `note` coexist in the update patch. Kept upstream 255-character alias editing, proxy catalog selection, fork 2048-character notes, and English/Chinese/Japanese labels.
- The detail panel tracks alias and note dirtiness independently. A pending/unsaved alias blocks the settings submit handler, including form submission by keyboard. Saving an alias keeps unsaved note/weight/proxy drafts.
- Classic PUT responses omit modern-only `note` and `weight_manual`. Existing request-value backfill still handles `note: null` as an empty display string. Detail saves retain their note; both detail and card alias-only saves preserve the current persisted note in emitted/cached rows. The latter requires a small change to `modern/features/groups/GroupCredentials.vue` beyond the conflict files.

## Automatic merge audit

- Classic alias DTO allowlist, normalization, label projection and localized editor are present. The classic DTO remains strict and does not receive a modern-only note field.
- Modern `CredentialRow` and `readCredential` retain notes, add alias/label, and include proxy ID/name/reference status. Existing server-side note semantics are unchanged by the frontend.
- `rate_limit_reset_hint_enabled` remains registered through classic DTOs, strict allowlist, projection, clone/save draft and switch; modern runtime switches and all three locales retain it.
- Classic `GroupModelUsageTab` and modern `GroupModelUsageMatrix` remain mounted. Their unchanged projections retain actual window start/source, missing/partial usage counts and incomplete-data flags.
- Credential test response projections and three-language diagnostic messages retain upstream status, safe error details, retry delay, outcome and restore proof.
- `package.json` and `pnpm-lock.yaml` match the incoming upstream versions; no extra dependency update was made.

## Checks

All completed with exit code 0:

- `corepack pnpm --dir web install --frozen-lockfile` (pnpm 11.17.0).
- `corepack pnpm --dir web run lint` (ESLint and `check-modern-styles.mjs`).
- `corepack pnpm --dir web run format`.
- `corepack pnpm --dir web run build` (vue-tsc, tsc, Vite 8.3.1; 3918 transformed modules). Built `internal/webui/dist` is ready for Go verification.
- `git diff --check -- web`; no conflict markers remain under `web/`.

The first lint/build attempts hit the machine's pnpm 10.32.1 when package scripts invoked bare `pnpm`. Re-ran with a process-local PATH prefix pointing to `%TEMP%/gpt-load-upstream-20261008-pnpm-shims`, created with `corepack enable --install-directory <that-directory> pnpm`. No global configuration changed.

The first format check found five CRLF-only files: classic `app/resources/group-model-usage.ts` and `features/groups/usage/GroupModelUsageTab.vue`; modern `api/group-model-usage.ts`, `features/groups/GroupModelUsageMatrix.vue`, and `i18n/locales/group-detail.ts`. Before normalizing each file, a byte comparison proved its LF content exactly equalled `git show HEAD:<path>`. Only CRLF was changed to LF, so these files introduce no Git content diff.

No browser tests or frontend test framework were added, per frontend quality guidelines. Interactive browser behavior was not exercised. No files were staged or committed by this worker.
