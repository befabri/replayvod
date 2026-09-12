import { defineConfig } from 'astro/config';
import { execSync } from 'node:child_process';
import mdx from '@astrojs/mdx';
import starlight from '@astrojs/starlight';
import sitemap from '@astrojs/sitemap';

import tailwindcss from '@tailwindcss/vite';

const SITEMAP_EXCLUDE = /\/(account|checkout|login|auth)(\/|$)/;

function safeExec(cmd: string): string {
  try {
    return execSync(cmd, { stdio: ['ignore', 'pipe', 'ignore'] }).toString().trim();
  } catch {
    return '';
  }
}

// Injected at build time so the footer can show real provenance.
// PUBLIC_ prefix exposes them through `import.meta.env` to client components.
process.env.PUBLIC_GIT_SHA ||= safeExec('git rev-parse --short HEAD') || 'dev';
process.env.PUBLIC_GIT_DATE ||= safeExec('git log -1 --format=%cs');

// https://astro.build/config
export default defineConfig({
  site: 'https://replayvod.com',
  prefetch: {
    prefetchAll: true,
    defaultStrategy: 'viewport',
  },
  integrations: [
    sitemap({
      filter: (page) => !SITEMAP_EXCLUDE.test(new URL(page).pathname),
    }),
    starlight({
      title: 'ReplayVOD Docs',
      description: 'Install and operate ReplayVOD, the self-hosted Twitch VOD recorder.',
      disable404Route: true,
      customCss: ['./src/styles/starlight.css'],
      components: {
        Head: './src/components/StarlightHead.astro',
        SiteTitle: './src/components/StarlightSiteTitle.astro',
      },
      social: [
        {
          icon: 'github',
          label: 'GitHub',
          href: 'https://github.com/befabri/replayvod',
        },
      ],
      sidebar: [
        {
          label: 'Start',
          items: [
            { label: 'Introduction', slug: 'docs' },
            { label: 'Quickstart', slug: 'docs/quickstart' },
            { label: 'How it works', slug: 'docs/concepts' },
          ],
        },
        {
          label: 'Install',
          items: [
            { label: 'Install', slug: 'docs/install' },
            { label: 'Updating', slug: 'docs/updating' },
          ],
        },
        {
          label: 'Use',
          items: [
            { label: 'Watching recordings', slug: 'docs/library' },
            { label: 'Downloads & history', slug: 'docs/activity' },
            { label: 'Your account', slug: 'docs/account' },
          ],
        },
        {
          label: 'Configure',
          items: [
            { label: 'Configuration', slug: 'docs/configuration' },
            { label: 'Storage', slug: 'docs/storage' },
            { label: 'Recording', slug: 'docs/recording' },
            { label: 'Schedules & title tracking', slug: 'docs/schedules' },
            { label: 'Archive VODs', slug: 'docs/archive' },
            { label: 'Twitch playback account', slug: 'docs/twitch-playback' },
            { label: 'Playback cache', slug: 'docs/playback-cache' },
            { label: 'EventSub', slug: 'docs/eventsub' },
            { label: 'Recording webhook', slug: 'docs/webhook' },
            { label: 'Connect relay', slug: 'docs/connect' },
          ],
        },
        {
          label: 'Operations',
          items: [
            { label: 'Users & invitations', slug: 'docs/access' },
            { label: 'Backup & restore', slug: 'docs/backup' },
            { label: 'Troubleshooting', slug: 'docs/troubleshooting' },
          ],
        },
        {
          label: 'Reference',
          items: [
            { label: 'Environment variables', slug: 'docs/reference/env' },
            { label: 'config.toml', slug: 'docs/reference/config-toml' },
          ],
        },
        {
          label: 'Contributing',
          items: [
            { label: 'Architecture', slug: 'docs/contributing/architecture' },
            { label: 'Development', slug: 'docs/contributing/development' },
            { label: 'Relay protocol', slug: 'docs/contributing/relay-protocol' },
          ],
        },
      ],
    }),
    // Starlight adds this itself, but without `gfm` the MDX pipeline drops tables.
    // Must stay after starlight() so expressive-code is registered first.
    mdx({ optimize: true, gfm: true }),
  ],
  vite: {
    plugins: [tailwindcss()]
  }
});
