import { describe, it, expect } from 'vitest'
import { layoutBoard, pixelCenter, cornersToPoints, HEX_SIZE } from './mapGeometry.js'

const tiles = [
  { id: 't-0-0', coord: { q: 0, r: 0 } },
  { id: 't-1-0', coord: { q: 1, r: 0 } },
  { id: 't-0-1', coord: { q: 0, r: 1 } },
  { id: 't-2-0', coord: { q: 2, r: 0 } },
]

function dist(a, b) {
  return Math.hypot(a.x - b.x, a.y - b.y)
}

describe('layoutBoard', () => {
  it('produces a hex per tile, keyed by id', () => {
    const { hexes, byId } = layoutBoard(tiles)
    expect(hexes).toHaveLength(tiles.length)
    for (const t of tiles) {
      expect(byId[t.id]).toBeTruthy()
      expect(byId[t.id].id).toBe(t.id)
    }
    expect(Object.keys(byId)).toHaveLength(tiles.length)
  })

  it('reports the configured hex size', () => {
    expect(layoutBoard(tiles).hexSize).toBe(HEX_SIZE)
  })

  it('returns a viewBox of four finite numbers', () => {
    const { viewBox } = layoutBoard(tiles)
    expect(typeof viewBox).toBe('string')
    const parts = viewBox.trim().split(/\s+/).map(Number)
    expect(parts).toHaveLength(4)
    for (const n of parts) expect(Number.isFinite(n)).toBe(true)
    // width and height must be positive.
    expect(parts[2]).toBeGreaterThan(0)
    expect(parts[3]).toBeGreaterThan(0)
  })

  it('gives distinct pixel centres to distinct axial coordinates', () => {
    const { byId } = layoutBoard(tiles)
    const centres = tiles.map((t) => `${byId[t.id].cx.toFixed(4)},${byId[t.id].cy.toFixed(4)}`)
    expect(new Set(centres).size).toBe(tiles.length)
  })

  it('falls back to a sane viewBox for an empty board', () => {
    const { hexes, viewBox } = layoutBoard([])
    expect(hexes).toHaveLength(0)
    const parts = viewBox.trim().split(/\s+/).map(Number)
    expect(parts).toHaveLength(4)
    for (const n of parts) expect(Number.isFinite(n)).toBe(true)
  })
})

describe('pixelCenter', () => {
  it('returns finite numbers for the origin', () => {
    const c = pixelCenter(0, 0)
    expect(Number.isFinite(c.x)).toBe(true)
    expect(Number.isFinite(c.y)).toBe(true)
  })

  it('places two adjacent coordinates closer than two distant ones', () => {
    const origin = pixelCenter(0, 0)
    const adjacent = pixelCenter(1, 0)
    const distant = pixelCenter(2, 0)
    expect(dist(origin, adjacent)).toBeLessThan(dist(origin, distant))
  })

  it('gives distinct centres to distinct coordinates', () => {
    const a = pixelCenter(0, 0)
    const b = pixelCenter(1, 0)
    const c = pixelCenter(0, 1)
    expect(a).not.toEqual(b)
    expect(a).not.toEqual(c)
    expect(b).not.toEqual(c)
  })
})

describe('cornersToPoints', () => {
  it('serializes a hex\'s corners into an SVG points string', () => {
    const { hexes } = layoutBoard(tiles)
    const points = cornersToPoints(hexes[0].corners)
    expect(typeof points).toBe('string')
    // Six corners => six "x,y" pairs separated by spaces.
    expect(points.split(' ')).toHaveLength(6)
    expect(points).toMatch(/^-?\d+(\.\d+)?,-?\d+(\.\d+)?/)
  })
})
