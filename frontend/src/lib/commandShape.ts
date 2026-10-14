// Shell prompt prefixes (PowerShell, bash, zsh) and sudo wrapper
const PROMPT = /^(?:PS[^>\n]*>|>|\$|❯)\s+/
const SUDO = /^sudo\s+/

// Matches curl CLI execution (including curl.exe for Windows)
const COMMAND = /^curl(?:\.exe)?\b/i

/**
 * Fast synchronous check to determine if pasted text resembles a curl command.
 * Strips leading shell prompts/sudo before matching. Used in paste handlers to
 * decide whether to call event.preventDefault() for async backend parsing.
 */
export function looksLikeCommand(text: string): boolean {
  let rest = text.trimStart()
  for (;;) {
    const stripped = rest.replace(PROMPT, '').replace(SUDO, '')
    if (stripped === rest) break
    rest = stripped
  }
  return COMMAND.test(rest)
}