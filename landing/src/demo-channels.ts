/**
 * Fake channel handles for the marketing illustrations. Every made-up handle on the
 * page resolves through here, so renaming one here renames it everywhere it appears.
 *
 * Keys are the slot a handle fills; values are personal handles, the way a real Twitch
 * username reads. Deliberately not descriptive of what the channel streams.
 */
export const channels = {
  chatting: 'tamsin',
  development: 'korvo',
  music: 'sylvtv',
  creative: 'renn42',
  irl: 'kavu',
  technology: 'noxi',
  gaming: 'vessa',
} as const;
