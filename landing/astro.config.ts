import { defineConfig } from 'astro/config';
import { execSync } from 'node:child_process';
import starlight from '@astrojs/starlight';

import tailwindcss from '@tailwindcss/vite';

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
          label: 'Configure',
          items: [
            { label: 'Configuration', slug: 'docs/configuration' },
            { label: 'Storage', slug: 'docs/storage' },
            { label: 'Recording', slug: 'docs/recording' },
            { label: 'Schedules & title tracking', slug: 'docs/schedules' },
            { label: 'EventSub', slug: 'docs/eventsub' },
            { label: 'Connect relay', slug: 'docs/connect' },
          ],
        },
        {
          label: 'Operations',
          items: [
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
  ],
  vite: {
    plugins: [tailwindcss()]
  }
});
