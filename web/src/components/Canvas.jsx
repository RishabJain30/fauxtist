import { useRef, useEffect, useCallback } from 'react'

export default function Canvas({ strokes, canDraw, onStrokeComplete }) {
  const ref = useRef(null)
  const drawing = useRef(false)
  const current = useRef([])

  const redraw = useCallback(() => {
    const cv = ref.current
    if (!cv) return
    const ctx = cv.getContext('2d')
    const { width: w, height: h } = cv
    ctx.clearRect(0, 0, w, h)
    const paint = (pts, color, width) => {
      if (!pts || pts.length === 0) return
      ctx.strokeStyle = color || '#111'
      ctx.lineWidth = (width || 3) * (w / 800)
      ctx.lineJoin = ctx.lineCap = 'round'
      ctx.beginPath()
      pts.forEach((pt, i) => {
        const x = pt.x * w, y = pt.y * h
        i === 0 ? ctx.moveTo(x, y) : ctx.lineTo(x, y)
      })
      ctx.stroke()
    }
    strokes.forEach((s) => paint(s.points, s.color, s.width))
    paint(current.current, '#111', 3)
  }, [strokes])

  useEffect(() => { redraw() }, [redraw])

  useEffect(() => {
    const cv = ref.current
    if (!cv) return
    const resize = () => {
      cv.width = cv.clientWidth
      cv.height = cv.clientHeight
      redraw()
    }
    resize()
    window.addEventListener('resize', resize)
    return () => window.removeEventListener('resize', resize)
  }, [redraw])

  const pos = (e) => {
    const r = ref.current.getBoundingClientRect()
    return { x: (e.clientX - r.left) / r.width, y: (e.clientY - r.top) / r.height }
  }
  const start = (e) => { if (!canDraw) return; drawing.current = true; current.current = [pos(e)]; redraw() }
  const move = (e) => { if (!drawing.current) return; current.current.push(pos(e)); redraw() }
  const end = () => {
    if (!drawing.current) return
    drawing.current = false
    const pts = current.current
    current.current = []
    if (pts.length > 0) onStrokeComplete({ points: pts, color: '#111', width: 3 })
  }

  return (
    <canvas
      ref={ref}
      onPointerDown={start}
      onPointerMove={move}
      onPointerUp={end}
      onPointerLeave={end}
      style={{ cursor: canDraw ? 'crosshair' : 'default' }}
    />
  )
}
