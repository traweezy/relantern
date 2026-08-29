# Relantern repository instructions

The product specification at
`docs/PERSONAL_DEVELOPER_INTELLIGENCE_PLATFORM_BUILD_SPEC.md` is the product
and architecture authority. The owner-level `/home/tylers/.codex/AGENTS.md`
rules also remain binding.

## Repository invariants

- Use Go 1.27 and Node 24 LTS production toolchains exactly as recorded in
  `docs/version-manifest.md`.
- Use the single root pnpm workspace and lockfile. Do not use npm, Yarn, or Bun
  for installation.
- Use Biome. ESLint and Prettier packages or configuration are forbidden.
- MUI packages, imports, copied components, icons, themes, and transitive
  styling dependencies are forbidden.
- Keep the default local stack disconnected from live OpenAI, GitHub write,
  Discord, Resend, and Railway delivery APIs.
- Keep `packages/api-client`, `packages/domain`, and
  `packages/design-tokens` platform-neutral. They may not import Next.js,
  React DOM, Node built-ins, Expo, React Native, Radix, shadcn/ui, or Tailwind.
- Do not add `apps/mobile` before Version 1 Phase P10 passes.
- Only `web` may receive public application traffic in hosted environments.
- Treat `/demo` as a static fixture-only security boundary. It may not import
  authenticated data, auth-server, live transport, delivery, OpenAI, database,
  or private operations modules.
- Use forward-only production migrations and expand/backfill/contract for
  destructive schema evolution.
- Never add floating dependency versions, container tags, or GitHub Action
  tags to production inputs.

## Required gates

Run `make prepush` before declaring a change complete. UI changes additionally
require real rendered desktop screenshots and the visual checks specified by
the owner instructions.
