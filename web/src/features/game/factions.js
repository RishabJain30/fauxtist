// Faction identities are cosmetic and symmetric — colour, sigil, pattern, and
// short label only, never an ability. Ownership is always shown by colour PLUS
// sigil PLUS pattern so it never depends on colour alone (accessibility).
export const FACTIONS = {
  ember: { label: 'Ember', color: '#e8583f', accent: '#ffb48f', sigil: '▲', pattern: 'dots' },
  tide: { label: 'Tide', color: '#3b82c4', accent: '#8fc7f0', sigil: '≋', pattern: 'waves' },
  grove: { label: 'Grove', color: '#3f9d5a', accent: '#9fe0ac', sigil: '❧', pattern: 'leaf' },
  dusk: { label: 'Dusk', color: '#8b5cc4', accent: '#cbaef0', sigil: '☾', pattern: 'diag' },
  sun: { label: 'Sun', color: '#e0a92e', accent: '#ffe09a', sigil: '✸', pattern: 'rays' },
  frost: { label: 'Frost', color: '#4bb6c6', accent: '#a9eaef', sigil: '❄', pattern: 'grid' },
}

const NEUTRAL = { label: 'Neutral', color: '#8a94a6', accent: '#c3c9d4', sigil: '·', pattern: 'none' }

export function factionOf(id) {
  return FACTIONS[id] || NEUTRAL
}

// factionForPlayer resolves a player's faction cosmetics from a player view.
export function factionForPlayer(player) {
  if (!player || !player.faction) return NEUTRAL
  return factionOf(player.faction)
}
