// Tree keys outside the registry, whose combinations are the same on every platform while delete is
// Del on Windows and ⌘⌫ on macOS. Kept together so the menu chord and the handler never drift.

/** The chord the menu writes beside the item that starts a rename. */
export const renameChord = 'F2'

/** The chord the menu writes beside the item that removes a node. */
export function removeChord(mac: boolean): string {
  return mac ? '⌘⌫' : 'Del'
}

/** Whether this event is the rename key. */
export function isRenameEvent(event: KeyboardEvent): boolean {
  return event.key === 'F2'
}

/** Whether this event is the platform's remove key. */
export function isRemoveEvent(event: KeyboardEvent, mac: boolean): boolean {
  return mac ? event.metaKey && event.key === 'Backspace' : event.key === 'Delete'
}
