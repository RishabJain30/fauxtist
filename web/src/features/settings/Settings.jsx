import * as Dialog from '@radix-ui/react-dialog'
import { Settings as SettingsIcon, X } from 'lucide-react'

// Settings is the local-preferences dialog. Sound settings are entirely
// separate from voice-chat controls. Nothing sensitive is stored.
export function Settings({ prefs, setPref, trigger }) {
  return (
    <Dialog.Root>
      <Dialog.Trigger asChild>
        {trigger || (
          <button className="icon-btn" aria-label="Settings">
            <SettingsIcon size={18} aria-hidden="true" />
          </button>
        )}
      </Dialog.Trigger>
      <Dialog.Portal>
        <Dialog.Overlay className="dialog-overlay" />
        <Dialog.Content className="dialog-content">
          <div className="dialog-head">
            <Dialog.Title>Settings</Dialog.Title>
            <Dialog.Close asChild>
              <button className="icon-btn" aria-label="Close"><X size={18} /></button>
            </Dialog.Close>
          </div>
          <div className="settings-body">
            <label className="toggle-row">
              <span>Sound effects</span>
              <input type="checkbox" checked={prefs.sound} onChange={(e) => setPref('sound', e.target.checked)} />
            </label>
            <label className="field">
              <span>Sound volume</span>
              <input type="range" min="0" max="1" step="0.05" value={prefs.sfxVolume} disabled={!prefs.sound} onChange={(e) => setPref('sfxVolume', Number(e.target.value))} />
            </label>
            <label className="toggle-row">
              <span>Reduced motion</span>
              <input type="checkbox" checked={prefs.reducedMotion} onChange={(e) => setPref('reducedMotion', e.target.checked)} />
            </label>
            <label className="toggle-row">
              <span>High contrast</span>
              <input type="checkbox" checked={prefs.highContrast} onChange={(e) => setPref('highContrast', e.target.checked)} />
            </label>
            <label className="toggle-row">
              <span>Push-to-talk (hold Space)</span>
              <input type="checkbox" checked={prefs.pushToTalk} onChange={(e) => setPref('pushToTalk', e.target.checked)} />
            </label>
          </div>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  )
}
