import { usePlatform } from './usePlatform'
import { useSettings } from './useSettings'
import { capsOf, COMMANDS, isCombo, type Command } from '../lib/commands'

/** The keyboard as the window reads it: what a command is bound to now, and whether an event is it.
 *  What somebody changed is in the settings; everything else wears what the registry says. */
export function useCommands() {
  const { settings } = useSettings()
  const { isMac } = usePlatform()

  /** The combination a command wears right now. */
  function combo(id: string): string {
    const changed = settings.value?.shortcuts?.[id]
    if (changed !== undefined) return changed
    return COMMANDS.find((command) => command.id === id)?.combo ?? ''
  }

  /** Whether an event is a command's combination, as the keyboard just pressed it. */
  function matches(id: string, event: KeyboardEvent): boolean {
    return isCombo(combo(id), event, isMac.value)
  }

  /** The keycaps of a command's combination, in this platform's own letters. */
  function caps(id: string): string[] {
    return capsOf(combo(id), isMac.value)
  }

  /** The combination as a hint beside a command's name: ⌘K on macOS, Ctrl+K everywhere else. */
  function hint(id: string): string {
    return caps(id).join(isMac.value ? '' : '+')
  }

  /** Whether a command may be offered on this platform at all: the plain Ctrl+R is the webview's
   *  own reload on Windows and Linux, so the combination the run command comes with is not offered
   *  there — one somebody picks deliberately is theirs to keep. */
  function offered(id: string): boolean {
    return id !== 'run' || isMac.value || combo(id) !== 'mod+R'
  }

  return { commands: COMMANDS as readonly Command[], combo, matches, caps, hint, offered }
}
