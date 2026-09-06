import { defineHex } from 'honeycomb-grid'

// Hex rendering geometry. honeycomb-grid handles the axial→pixel conversion and
// polygon corners; the Go server remains authoritative for adjacency and every
// rule (this is rendering assistance only).

export const HEX_SIZE = 34

const Hex = defineHex({ dimensions: HEX_SIZE, orientation: 'pointy' })

// layoutBoard converts authoritative tiles (each { id, coord:{q,r}, ... }) into
// pixel-space hexes plus an SVG viewBox that frames them with padding.
export function layoutBoard(tiles) {
  const hexes = (tiles || []).map((t) => {
    const h = new Hex({ q: t.coord.q, r: t.coord.r })
    return {
      id: t.id,
      q: t.coord.q,
      r: t.coord.r,
      cx: h.x,
      cy: h.y,
      corners: h.corners.map((c) => ({ x: c.x, y: c.y })),
    }
  })

  let minX = Infinity
  let minY = Infinity
  let maxX = -Infinity
  let maxY = -Infinity
  for (const h of hexes) {
    for (const c of h.corners) {
      if (c.x < minX) minX = c.x
      if (c.y < minY) minY = c.y
      if (c.x > maxX) maxX = c.x
      if (c.y > maxY) maxY = c.y
    }
  }
  if (!isFinite(minX)) {
    minX = 0
    minY = 0
    maxX = HEX_SIZE
    maxY = HEX_SIZE
  }
  const pad = HEX_SIZE * 0.9
  const viewBox = `${minX - pad} ${minY - pad} ${maxX - minX + pad * 2} ${maxY - minY + pad * 2}`

  const byId = {}
  for (const h of hexes) byId[h.id] = h

  return { hexes, byId, viewBox, hexSize: HEX_SIZE }
}

// cornersToPoints serializes a hex's corners into an SVG points attribute.
export function cornersToPoints(corners) {
  return corners.map((c) => `${round(c.x)},${round(c.y)}`).join(' ')
}

function round(n) {
  return Math.round(n * 100) / 100
}

// pixelCenter returns the pixel center of an axial coordinate (for tests and
// standalone hit-testing).
export function pixelCenter(q, r) {
  const h = new Hex({ q, r })
  return { x: h.x, y: h.y }
}
