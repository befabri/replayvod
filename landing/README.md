# landing

The marketing site at <https://replayvod.com> and the account portal for
ReplayVOD Connect (login, magic link, billing). Built with Astro and shipped
as a static bundle.

This is _not_ the recorder dashboard — see [`dashboard/`](../dashboard/) for
that. The Connect cloud backend it talks to lives in
[`cloud/`](../cloud/).

## Contents

- [What this is](#what-this-is)
- [Stack](#stack)
- [Project layout](#project-layout)
- [Scripts](#scripts)
- [Configuration](#configuration)
- [Tests](#tests)
- [Deploy](#deploy)
- [License](#license)

## What this is

Two things in one Astro site:

1. **Marketing landing** — `/`, with hero, features, pricing, FAQ, install
   instructions, and legal pages.
2. **Connect account portal** — `/login`, `/auth/magic-link`, `/account`.
   Magic-link sign-in and a small dashboard for managing relay tokens and
   Polar subscriptions. Talks to the Connect Worker at
   `PUBLIC_CONNECT_API_URL` (defaults to `https://api.replayvod.com`). Public checkout and sign-in are disabled unless
   `PUBLIC_CONNECT_PAYMENTS_ENABLED=1` or `PUBLIC_CONNECT_ACCOUNT_ENABLED=1` is set at build time.

Connect is a strict webhook relay; this site is the only place users
interact with billing and tokens — the recorder itself never sees those.

## Stack

- **Astro 6** with the Starlight integration for the docs section
- **Tailwind CSS v4** via `@tailwindcss/vite`
- **TypeScript 5** in strict mode
- **Prettier** for formatting
- Node ≥ 22.12

Static-only — no SSR, no adapter.

## Project layout

```
landing/
├── src/
│   ├── pages/
│   │   ├── index.astro             # marketing landing page
│   │   ├── login.astro             # Connect sign-in (magic link request)
│   │   ├── auth/magic-link.astro   # magic-link consume
│   │   ├── account.astro           # subscription + relay token dashboard
│   │   └── legal/{privacy,terms,twitch}.astro
│   ├── layouts/
│   │   ├── Layout.astro            # base page chrome
│   │   └── Legal.astro             # legal pages chrome
│   ├── components/                 # marketing sections (see below)
│   ├── content/docs/               # Starlight docs source (Markdown)
│   ├── styles/                     # Tailwind entry + theme tokens
│   ├── assets/                     # images, logos
│   └── config.ts                   # runtime config defaults
├── tests/                          # docker-compose install tests
├── public/                         # static assets copied verbatim
└── astro.config.ts                 # site, Starlight, Tailwind, git provenance
```

Marketing components in `src/components/`:

| Component             | Role |
| --------------------- | ---- |
| `Nav.astro`           | top navigation |
| `Hero.astro`          | hero block |
| `FeatureGrid.astro`   | feature cards |
| `SplitLiveOps.astro`  | "live operations" split section |
| `SplitInstall.astro`  | install split section |
| `HowItWorks.astro`    | step-by-step diagram |
| `StatsBand.astro`     | usage stats banner |
| `Connect.astro`       | Connect relay pitch |
| `Pricing.astro`       | Connect tiers |
| `FAQ.astro`           | frequently asked questions |
| `CTABand.astro`       | final call-to-action |
| `Footer.astro`        | footer with git SHA + commit date |

## Scripts

| Script                 | What it does |
| ---------------------- | ------------ |
| `npm run dev`          | `astro dev` |
| `npm run build`        | `astro build` to `dist/` |
| `npm run preview`      | serve the production build locally |
| `npm run check`        | `astro check` (TypeScript + Astro diagnostics) |
| `npm run format`       | Prettier write |
| `npm run format:check` | Prettier check |
| `npm run test:install` | Docker-compose install tests under `tests/install/` |

## Configuration

Public build-time variables (exposed to the client as `import.meta.env.PUBLIC_*`):

| Variable                       | Default                          | Purpose |
| ------------------------------ | -------------------------------- | ------- |
| `PUBLIC_CONNECT_API_URL`       | `https://api.replayvod.com`      | Connect cloud API base URL |
| `PUBLIC_CONNECT_PAYMENTS_ENABLED` | disabled                       | Set to `1` to show checkout CTAs |
| `PUBLIC_CONNECT_ACCOUNT_ENABLED` | disabled                        | Set to `1` to show sign-in/account access without checkout |
| `PUBLIC_OPERATOR_LEGAL_NAME`   | `ReplayVOD`                      | legal entity in privacy / terms |
| `PUBLIC_OPERATOR_ADDRESS`      | `Available on request at legal@replayvod.com` | operator address |
| `PUBLIC_OPERATOR_JURISDICTION` | `the operator's registered jurisdiction` | legal jurisdiction |
| `PUBLIC_GIT_SHA`               | `git rev-parse --short HEAD`     | injected at build, shown in footer |
| `PUBLIC_GIT_DATE`              | `git log -1 --format=%cs`        | injected at build, shown in footer |

Defaults live in `src/config.ts` and `astro.config.ts`.

## Tests

`tests/install/` contains a docker-compose smoke test that exercises the
install scripts the landing page links to. Run with:

```bash
npm run test:install
```

## Deploy

Pure static output; `npm run build` writes pre-rendered HTML, CSS, and JS to
`dist/`. Drop it on Cloudflare Pages, Vercel Static, or any HTTP host. The
configured site URL is `https://replayvod.com` (`astro.config.ts`).

## License

[GPL-3.0](../LICENSE), like the rest of the project.
