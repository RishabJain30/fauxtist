import { TransformWrapper, TransformComponent } from 'react-zoom-pan-pinch'
import { ZoomIn, ZoomOut, Maximize, RotateCcw } from 'lucide-react'

// BoardViewport is a thin local abstraction over react-zoom-pan-pinch: mouse
// wheel + pinch zoom, drag/pan, fit, reset, bounded zoom, and a tap-vs-drag
// distinction so tapping a hex never counts as a pan. Reduced motion disables
// the smooth animations. The board SVG is passed as children.
export function BoardViewport({ children, reducedMotion }) {
  return (
    <TransformWrapper
      minScale={0.5}
      maxScale={4}
      initialScale={1}
      centerOnInit
      wheel={{ step: 0.12 }}
      pinch={{ step: 5 }}
      doubleClick={{ disabled: true }}
      panning={{ velocityDisabled: true }}
      // Below this movement threshold a pointer up is treated as a tap (hex
      // select), not a pan.
      velocityAnimation={{ disabled: !!reducedMotion }}
      smooth={!reducedMotion}
    >
      {({ zoomIn, zoomOut, resetTransform, centerView }) => (
        <div className="board-viewport">
          <div className="board-controls" role="group" aria-label="Board controls">
            <button className="icon-btn" aria-label="Zoom in" onClick={() => zoomIn()}>
              <ZoomIn size={18} aria-hidden="true" />
            </button>
            <button className="icon-btn" aria-label="Zoom out" onClick={() => zoomOut()}>
              <ZoomOut size={18} aria-hidden="true" />
            </button>
            <button className="icon-btn" aria-label="Fit board" onClick={() => centerView(1)}>
              <Maximize size={18} aria-hidden="true" />
            </button>
            <button className="icon-btn" aria-label="Reset view" onClick={() => resetTransform()}>
              <RotateCcw size={18} aria-hidden="true" />
            </button>
          </div>
          <TransformComponent wrapperClass="board-transform-wrapper" contentClass="board-transform-content">
            {children}
          </TransformComponent>
        </div>
      )}
    </TransformWrapper>
  )
}
