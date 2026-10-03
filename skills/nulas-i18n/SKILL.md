---
name: nulas-i18n
description: Implement and maintain Nulas interface translations, language selection and locale-aware formatting. Use when adding UI copy, adding a language or changing localization behavior in this project.
---

# Nulas localization

Nulas uses SolidStart SSR and SolidJS. Read `web/src/lib/i18n.tsx`, `web/src/lib/translation.ts` and the relevant component before changing localization.

## Current design

- Simplified Chinese (`zh-CN`) is the source language and SSR default. English (`en`) translations live in `web/src/lib/locales/en.ts`. Chinese source phrases are the message keys; shared phrases share entries. Keep whole sentences together when their word order matters.
- Call `useI18n()` inside components, then `t(sourcePhrase, parameters)` in reactive render expressions. Avoid module-level translated values: they freeze a language and can leak across SSR requests. Static mode/status maps contain source phrases; translate their values when rendering.
- Parameterized messages use `{name}` placeholders (existing migrated messages use `{p0}`, `{p1}`). Preserve the exact parameter set in each language and prefer meaningful parameter names in new copy. Pass user values separately; never translate node names, profile names, protocol values, configuration keys, URLs, credentials or shell commands. Solid escapes rendered strings; do not use `innerHTML` for translations.
- The provider owns a signal per application/request. SSR and first hydration both use Chinese; `onMount` resolves the saved `nulas-language` cookie, then browser language preferences, then Chinese. The selector changes language immediately and saves a one-year cookie with path `/` and SameSite=Lax. A brief Chinese first render on English browsers is an intentional current tradeoff. If server-side locale negotiation is introduced, serialize the same initial locale into hydration and vary cached HTML appropriately.
- Update document `lang`, page titles, accessible labels, placeholders, empty/loading states, options, warnings and dates as part of UI localization. Format dates with `locale()`; never change machine-readable values or persisted timestamps.
- Backend task messages, runtime diagnostics and third-party errors remain raw. Do not guess translations from arbitrary backend prose. Localizing those requires stable error/message codes and parameters while preserving historical job records. Frontend notices created by an action currently retain the language used when that action ran.
- Language is a browser presentation preference. Do not reuse it as observed network state or change the backend's durable tray/proxy/TUN/startup preference handling.

## Adding copy or a language

Add each new source phrase and its English translation together. Keep safety and platform limitations equally explicit in both languages, including full-profile disabling of TUN/transparent proxy, manual retries and background execution. Prefer a complete interpolated sentence over concatenated translated fragments.

For another language, add its dictionary, extend the locale list and normalization, update dictionary selection and the native-language selector option, and preserve Chinese fallback for unknown languages/messages. Keep date formatting compatible with the locale identifier. Avoid a new localization dependency unless the requirements justify it.

Run `cd web && pnpm test:i18n`, `pnpm typecheck` and `pnpm build`. The tests check language resolution, safe interpolation, catalog coverage and placeholder parity. Exercise switching on the affected pages when browser verification is available; check navigation, reload persistence, title/lang and long English copy. Do not enable networking features or install cores merely to verify translations.
