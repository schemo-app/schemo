import type { MessageKey } from '../i18n'

// The commands the window knows, with the combination each comes with: the one place a shortcut is
// written down. Every handler asks for its command rather than carrying its own key, so what the
// settings draw is what the keyboard does.
//
// A combination is modifiers then a key: `mod` is the platform's own (⌘ on macOS, Ctrl elsewhere),
// and the key is a letter, a digit, a named key such as `enter`, or an F-key. Go checks the shape
// before it keeps one, so both sides agree on what a combination is.

/** The combination of a command somebody unassigned: it wears none, and no keyboard can match it. */
export const COMBO_NONE = 'none'

export interface Command {
  id: string
  group: MessageKey
  label: MessageKey
  combo: string
}

export const COMMANDS: readonly Command[] = [
  { id: 'palette', group: 'shortcuts.groups.general', label: 'shortcuts.cmd.palette', combo: 'mod+K' },
  { id: 'settings', group: 'shortcuts.groups.general', label: 'shortcuts.cmd.settings', combo: 'mod+,' },
  { id: 'sidebar', group: 'shortcuts.groups.general', label: 'shortcuts.cmd.sidebar', combo: 'mod+B' },
  { id: 'updates', group: 'shortcuts.groups.general', label: 'shortcuts.cmd.updates', combo: 'mod+U' },
  { id: 'history', group: 'shortcuts.groups.general', label: 'shortcuts.cmd.history', combo: 'mod+Y' },

  { id: 'send', group: 'shortcuts.groups.request', label: 'shortcuts.cmd.send', combo: 'mod+enter' },
  { id: 'save', group: 'shortcuts.groups.request', label: 'shortcuts.cmd.save', combo: 'mod+S' },
  { id: 'environments', group: 'shortcuts.groups.request', label: 'shortcuts.cmd.environments', combo: 'mod+E' },
  { id: 'newRequest', group: 'shortcuts.groups.request', label: 'shortcuts.cmd.newRequest', combo: 'mod+N' },
  { id: 'duplicate', group: 'shortcuts.groups.request', label: 'shortcuts.cmd.duplicate', combo: 'mod+D' },

  { id: 'run', group: 'shortcuts.groups.collections', label: 'shortcuts.cmd.run', combo: 'mod+R' },
  { id: 'import', group: 'shortcuts.groups.collections', label: 'shortcuts.cmd.import', combo: 'mod+O' },
  { id: 'export', group: 'shortcuts.groups.collections', label: 'shortcuts.cmd.export', combo: 'mod+shift+E' },

  { id: 'newTab', group: 'shortcuts.groups.tabs', label: 'shortcuts.cmd.newTab', combo: 'mod+T' },
  { id: 'closeTab', group: 'shortcuts.groups.tabs', label: 'shortcuts.cmd.closeTab', combo: 'mod+W' },
  { id: 'closeOthers', group: 'shortcuts.groups.tabs', label: 'shortcuts.cmd.closeOthers', combo: 'mod+alt+W' },

  { id: 'find', group: 'shortcuts.groups.response', label: 'shortcuts.cmd.find', combo: 'mod+F' },
  { id: 'inspector', group: 'shortcuts.groups.response', label: 'shortcuts.cmd.inspector', combo: 'mod+alt+I' },
]

/** The combination a keyboard event is, or nothing for one that cannot be one: modifiers alone,
 *  unmodified keys that would swallow typing, the other platform's modifier, and a character typed
 *  with AltGr. On macOS Control belongs to the text field's own caret keys (Ctrl+K, Ctrl+E), and on
 *  the other platforms the Windows key is the system's. */
export function comboOfEvent(event: KeyboardEvent, mac: boolean): string {
  if (['Control', 'Meta', 'Alt', 'Shift'].includes(event.key)) return ''
  if (mac ? event.ctrlKey : event.metaKey) return ''
  if (event.getModifierState?.('AltGraph')) return ''

  const key = keyOfEvent(event)
  const mods: string[] = []
  if (mac ? event.metaKey : event.ctrlKey) mods.push('mod')
  if (event.altKey) mods.push('alt')
  if (event.shiftKey) mods.push('shift')
  if (!mods.length && !/^F\d+$/.test(key)) return ''

  return [...mods, key].join('+')
}

// The physical key, not the letter: on a Russian layout this key carries a Cyrillic letter, and the
// combination recorded on one layout is the same key on another.
function keyOfEvent(event: KeyboardEvent): string {
  if (/^Key[A-Z]$/.test(event.code)) return event.code.slice(3)
  if (/^Digit\d$/.test(event.code)) return event.code.slice(5)
  if (event.code === 'Comma') return ','
  if (event.key === 'Enter') return 'enter'
  return event.key.length === 1 ? event.key.toUpperCase() : event.key
}

/** Whether a keyboard event is exactly this combination — the same modifiers, no others. */
export function isCombo(combo: string, event: KeyboardEvent, mac: boolean): boolean {
  if (!combo || combo === COMBO_NONE) return false
  return combo === comboOfEvent(event, mac)
}

/** The keycaps of a combination, in the order the platform writes its modifiers. */
export function capsOf(combo: string, mac: boolean): string[] {
  if (!combo || combo === COMBO_NONE) return []

  const parts = combo.split('+')
  const key = parts.pop() ?? ''
  const name: Record<string, string> = mac
    ? { alt: '⌥', shift: '⇧', mod: '⌘' }
    : { mod: 'Ctrl', alt: 'Alt', shift: 'Shift' }
  const order = mac ? ['alt', 'shift', 'mod'] : ['mod', 'alt', 'shift']

  const cap = key === 'enter' ? (mac ? '↵' : 'Enter') : key
  return [...order.filter((mod) => parts.includes(mod)).map((mod) => name[mod]), cap]
}

/** The same combination in the words the system's own menus take, for the accelerators that are not
 *  the window's to draw. */
export function acceleratorOf(combo: string): string {
  if (!combo) return ''
  if (combo === COMBO_NONE) return COMBO_NONE
  return combo
    .split('+')
    .map((part) => ({ mod: 'CmdOrCtrl', alt: 'Alt', shift: 'Shift' })[part] ?? part)
    .join('+')
}
