const connectPaymentsEnabled = import.meta.env.PUBLIC_CONNECT_PAYMENTS_ENABLED === '1';
const connectAccountEnabled =
  connectPaymentsEnabled || import.meta.env.PUBLIC_CONNECT_ACCOUNT_ENABLED === '1';

export const site = {
  name: 'ReplayVOD',
  tagline: 'Self-hosted Twitch VOD recorder',
  github: 'https://github.com/befabri/replayvod',
  // Set per environment. The legal pages and contact links pull from here.
  contact: {
    email: 'support@replayvod.com',
    legal: 'legal@replayvod.com',
  },
  // Fill in before publishing the legal pages. Referenced verbatim in /legal/terms and
  // /legal/privacy. Override with PUBLIC_* build variables on Cloudflare Pages for the
  // production legal entity.
  operator: {
    legalName: import.meta.env.PUBLIC_OPERATOR_LEGAL_NAME ?? 'ReplayVOD',
    address: import.meta.env.PUBLIC_OPERATOR_ADDRESS ?? 'Available on request at legal@replayvod.com',
    jurisdiction: import.meta.env.PUBLIC_OPERATOR_JURISDICTION ?? "the operator's registered jurisdiction",
  },
  connect: {
    apiBase: import.meta.env.PUBLIC_CONNECT_API_URL ?? 'https://api.replayvod.com',
    checkoutPath: '/checkout',
    paymentsEnabled: connectPaymentsEnabled,
    accountEnabled: connectAccountEnabled,
  },
  lastUpdated: '2026-04-28',
} as const;
