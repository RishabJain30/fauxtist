import * as Dialog from '@radix-ui/react-dialog'
import { HelpCircle, X } from 'lucide-react'
import { BRAND } from '../../app/brand.js'

// Tutorial is the How-to-play dialog, reachable from the lobby and in-game.
export function Tutorial({ trigger }) {
  return (
    <Dialog.Root>
      <Dialog.Trigger asChild>
        {trigger || (
          <button className="btn-ghost">
            <HelpCircle size={16} aria-hidden="true" /> How to play
          </button>
        )}
      </Dialog.Trigger>
      <Dialog.Portal>
        <Dialog.Overlay className="dialog-overlay" />
        <Dialog.Content className="dialog-content" aria-describedby="howto-desc">
          <div className="dialog-head">
            <Dialog.Title>How to play {BRAND.name}</Dialog.Title>
            <Dialog.Close asChild>
              <button className="icon-btn" aria-label="Close"><X size={18} /></button>
            </Dialog.Close>
          </div>
          <div id="howto-desc" className="howto-body">
            <p><strong>Goal.</strong> Control relics. Hold at least three of the five relics at two round-ends in a row to win by Domination — otherwise the most Influence after the final round wins.</p>
            <ol>
              <li><strong>Income.</strong> Collect energy (from round two on).</li>
              <li><strong>Negotiation.</strong> Talk, ping, and propose moves. Nothing binds.</li>
              <li><strong>Declaration.</strong> Commit one order everyone will see.</li>
              <li><strong>Reveal.</strong> All declarations become public.</li>
              <li><strong>Secret planning.</strong> Submit your hidden orders. Once per match you may turn your public declaration into a <em>Faux Order</em> — it won&apos;t execute, and you secretly do something else instead.</li>
              <li><strong>Resolution.</strong> Every order resolves at once. No dice — combat is deterministic.</li>
            </ol>
            <p><strong>Orders.</strong> March armies to adjacent tiles, Fortify for defence, Recruit at a capital or fortress, or Build a Fortress or Mine. You resolve three real orders each round.</p>
            <p><strong>Combat.</strong> An attacker takes a tile only with a unique strength strictly greater than the defender (garrison + fortress + fortify). Ties fail. Your capital can never be captured.</p>
          </div>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  )
}
