// Client-side order-draft helpers. Everything here is ADVISORY — it mirrors
// the server's rules to give immediate feedback (costs, legal targets,
// remaining slots) while building an order, but the Go server re-validates and
// is the sole authority. Never trust these results for anything but UX.

export const REAL_COMMAND_SLOTS = 3
export const MARCH_MIN = 1
export const MARCH_MAX = 3

export const COMMAND_COST = {
  march: 0,
  fortify: 1,
  recruit: 3,
  build_fortress: 3,
  build_mine: 4,
  hold: 0,
}

export const COMMAND_LABELS = {
  march: 'March',
  fortify: 'Fortify',
  recruit: 'Recruit',
  build_fortress: 'Build Fortress',
  build_mine: 'Build Mine',
  hold: 'Hold',
}

export function hexDistance(a, b) {
  const dq = a.q - b.q
  const dr = a.r - b.r
  return (Math.abs(dq) + Math.abs(dq + dr) + Math.abs(dr)) / 2
}

export function tileById(board, id) {
  return (board || []).find((t) => t.id === id) || null
}

export function ownedTiles(board, meId) {
  return (board || []).filter((t) => t.owner === meId)
}

// adjacentTiles returns the ids of board tiles neighbouring the given tile.
export function adjacentTiles(board, id) {
  const from = tileById(board, id)
  if (!from) return []
  return (board || []).filter((t) => t.id !== id && hexDistance(t.coord, from.coord) === 1).map((t) => t.id)
}

// legalMarchTargets returns adjacent tiles a March may target: any adjacent
// tile except an enemy capital.
export function legalMarchTargets(board, fromId, meId) {
  const from = tileById(board, fromId)
  if (!from || from.owner !== meId) return []
  return adjacentTiles(board, fromId).filter((id) => {
    const t = tileById(board, id)
    if (!t) return false
    if (t.type === 'capital' && t.capitalOwner !== meId) return false
    return true
  })
}

// commandCost returns a command's energy cost.
export function commandCost(cmd) {
  return COMMAND_COST[cmd.type] ?? 0
}

// realCommands assembles the full ordered real-command list for previews: the
// declaration first (unless Faux), then the hidden commands.
export function realCommands(declaration, hidden, faux) {
  const out = []
  if (!faux && declaration && declaration.type && declaration.type !== 'hold') out.push(declaration)
  return out.concat(hidden || [])
}

// draftEnergy sums the energy a full draft reserves. A Faux declaration costs
// nothing.
export function draftEnergy(declaration, hidden, faux) {
  return realCommands(declaration, hidden, faux).reduce((sum, c) => sum + commandCost(c), 0)
}

// slotsUsed / slotsRemaining count real command slots (Faux declaration does
// not consume one).
export function slotsUsed(declaration, hidden, faux) {
  return realCommands(declaration, hidden, faux).length
}

export function slotsRemaining(declaration, hidden, faux) {
  return Math.max(0, REAL_COMMAND_SLOTS - slotsUsed(declaration, hidden, faux))
}

// draftToWire converts hidden commands to the set_orders payload commands.
export function draftToWire(hidden) {
  return (hidden || []).map((c) => ({
    type: c.type,
    from: c.from || '',
    to: c.to || '',
    armies: c.armies || 0,
  }))
}

// canAfford reports whether a player's energy covers a full draft.
export function canAfford(energy, declaration, hidden, faux) {
  return draftEnergy(declaration, hidden, faux) <= (energy ?? 0)
}
