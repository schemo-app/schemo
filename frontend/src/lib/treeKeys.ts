// Two keys of the tree that are not commands of the registry, so they need a home of their own —
// and the chord the menu draws and the key the window answers must not drift apart, which is why
// both live here.
//
// They stay out of `commands.ts` because that registry writes one combination for every platform,
// and the second of these two is Del on Windows and ⌘⌫ on macOS; a second reason is that Go checks a
// stored combination's shape and knows no `delete` and no `backspace`.

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
