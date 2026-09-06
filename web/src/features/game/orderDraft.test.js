import { describe, it, expect } from 'vitest'
import {
  hexDistance,
  adjacentTiles,
  legalMarchTargets,
  commandCost,
  draftEnergy,
  slotsRemaining,
  canAfford,
  COMMAND_COST,
  REAL_COMMAND_SLOTS,
} from './orderDraft.js'

// A small hand-built board centred on (0,0) with all six axial neighbours
// present, plus a distant tile at (2,0). Tile shape mirrors the server's:
// { id, coord:{q,r}, type, owner, armies, structure, capitalOwner }.
function board() {
  return [
    { id: 'src', coord: { q: 0, r: 0 }, type: 'normal', owner: 'me' },
    { id: 'friendly', coord: { q: 1, r: 0 }, type: 'normal', owner: 'me' },
    { id: 'neutral', coord: { q: 0, r: 1 }, type: 'normal', owner: null },
    { id: 'enemyNormal', coord: { q: -1, r: 0 }, type: 'normal', owner: 'enemy' },
    { id: 'enemyCapital', coord: { q: 1, r: -1 }, type: 'capital', owner: 'enemy', capitalOwner: 'enemy' },
    { id: 'myCapital', coord: { q: -1, r: 1 }, type: 'capital', owner: 'me', capitalOwner: 'me' },
    { id: 'far', coord: { q: 2, r: 0 }, type: 'normal', owner: 'enemy' },
  ]
}

describe('hexDistance', () => {
  it('is zero for a coordinate against itself', () => {
    expect(hexDistance({ q: 0, r: 0 }, { q: 0, r: 0 })).toBe(0)
  })
  it('is one for an adjacent coordinate', () => {
    expect(hexDistance({ q: 0, r: 0 }, { q: 1, r: 0 })).toBe(1)
  })
  it('is two across two hexes on an axis', () => {
    expect(hexDistance({ q: 0, r: 0 }, { q: 2, r: 0 })).toBe(2)
  })
})

describe('adjacentTiles', () => {
  it('finds exactly the six neighbours present on the board, excluding self and distant tiles', () => {
    const ids = adjacentTiles(board(), 'src').sort()
    expect(ids).toEqual(['enemyCapital', 'enemyNormal', 'friendly', 'myCapital', 'neutral'].sort())
    expect(ids).not.toContain('src')
    expect(ids).not.toContain('far')
    expect(ids).toHaveLength(5) // five of the six neighbour slots are filled on this board
  })

  it('finds all six when every neighbour is present', () => {
    const full = [
      { id: 'c', coord: { q: 0, r: 0 } },
      { id: 'n1', coord: { q: 1, r: 0 } },
      { id: 'n2', coord: { q: -1, r: 0 } },
      { id: 'n3', coord: { q: 0, r: 1 } },
      { id: 'n4', coord: { q: 0, r: -1 } },
      { id: 'n5', coord: { q: 1, r: -1 } },
      { id: 'n6', coord: { q: -1, r: 1 } },
    ]
    expect(adjacentTiles(full, 'c').sort()).toEqual(['n1', 'n2', 'n3', 'n4', 'n5', 'n6'])
  })
})

describe('legalMarchTargets', () => {
  it('returns adjacent friendly, neutral and enemy-normal tiles plus a friendly capital', () => {
    const targets = legalMarchTargets(board(), 'src', 'me').sort()
    expect(targets).toEqual(['enemyNormal', 'friendly', 'myCapital', 'neutral'].sort())
  })

  it('excludes an adjacent enemy capital', () => {
    expect(legalMarchTargets(board(), 'src', 'me')).not.toContain('enemyCapital')
  })

  it('excludes non-adjacent tiles', () => {
    expect(legalMarchTargets(board(), 'src', 'me')).not.toContain('far')
  })

  it('returns nothing when the source is not owned by me', () => {
    expect(legalMarchTargets(board(), 'enemyNormal', 'me')).toEqual([])
  })
})

describe('commandCost / COMMAND_COST', () => {
  it('matches the documented energy costs', () => {
    expect(COMMAND_COST).toMatchObject({
      march: 0,
      fortify: 1,
      recruit: 3,
      build_fortress: 3,
      build_mine: 4,
      hold: 0,
    })
    expect(commandCost({ type: 'march' })).toBe(0)
    expect(commandCost({ type: 'fortify' })).toBe(1)
    expect(commandCost({ type: 'recruit' })).toBe(3)
    expect(commandCost({ type: 'build_fortress' })).toBe(3)
    expect(commandCost({ type: 'build_mine' })).toBe(4)
    expect(commandCost({ type: 'hold' })).toBe(0)
  })

  it('treats an unknown command as free', () => {
    expect(commandCost({ type: 'nonsense' })).toBe(0)
  })
})

describe('draftEnergy', () => {
  const declaration = { type: 'recruit' } // 3
  const hidden = [{ type: 'fortify' }, { type: 'build_mine' }] // 1 + 4

  it('sums the declaration plus hidden commands when not faux', () => {
    expect(draftEnergy(declaration, hidden, false)).toBe(3 + 1 + 4)
  })

  it('omits the declaration cost when it is faux', () => {
    expect(draftEnergy(declaration, hidden, true)).toBe(1 + 4)
  })

  it('does not count a hold declaration', () => {
    expect(draftEnergy({ type: 'hold' }, hidden, false)).toBe(1 + 4)
  })
})

describe('slotsRemaining', () => {
  it('subtracts the declaration and hidden commands from the slot budget', () => {
    expect(slotsRemaining({ type: 'recruit' }, [{ type: 'fortify' }], false)).toBe(REAL_COMMAND_SLOTS - 2)
  })

  it('does not consume a slot for a faux declaration', () => {
    expect(slotsRemaining({ type: 'recruit' }, [{ type: 'fortify' }], true)).toBe(REAL_COMMAND_SLOTS - 1)
  })

  it('leaves the full budget for a hold declaration with no hidden commands', () => {
    expect(slotsRemaining({ type: 'hold' }, [], false)).toBe(REAL_COMMAND_SLOTS)
  })
})

describe('canAfford', () => {
  const declaration = { type: 'recruit' } // 3
  const hidden = [{ type: 'fortify' }, { type: 'build_mine' }] // 5, total 8

  it('is true when energy covers the draft exactly', () => {
    expect(canAfford(8, declaration, hidden, false)).toBe(true)
  })

  it('is false when energy falls short', () => {
    expect(canAfford(7, declaration, hidden, false)).toBe(false)
  })

  it('accounts for a faux declaration being free', () => {
    expect(canAfford(5, declaration, hidden, true)).toBe(true)
  })
})
