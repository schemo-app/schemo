import { computed, ref } from 'vue'
import { McpService } from '../../bindings/schemo/internal/transport/wails'
import type { ConfirmRequest, Status } from '../../bindings/schemo/internal/transport/mcp'
import { useWailsEvent } from './events/useWailsEvent'

// The MCP server as the window draws it: whether it listens and who has called, followed by the
// event the server publishes whenever either moves — and the question it asks before a send, which
// is answered here and nowhere else.

const status = ref<Status | null>(null)
const question = ref<ConfirmRequest | null>(null)

useWailsEvent('mcp:changed', (ev) => {
  status.value = (ev.data as Status | null) ?? null
})

useWailsEvent('mcp:confirm', (ev) => {
  question.value = (ev.data as ConfirmRequest | null) ?? null
})

const running = computed(() => status.value?.running === true)
const busy = computed(() => status.value?.running !== true && status.value?.busy === true)
const agents = computed(() => status.value?.agents ?? [])

/** Composable for the MCP server's state and the two answers the window owes it. */
export function useMcp() {
  return {
    status,
    running,
    busy,
    agents,
    question,
    loadMCP,
    disconnectAgent,
    answerSend,
    revealToken,
  }
}

/** The token itself, for the eye beside it. Every answer carries `hasToken` instead: the value
    arrives only when somebody asks for it, and it is held by whoever asked. */
async function revealToken(): Promise<string> {
  return await McpService.RevealToken()
}

/** Reads the state once on open: the events arrive when something changes, and a window that opens
    after an agent has already called has nothing coming to it. */
async function loadMCP(): Promise<void> {
  try {
    status.value = await McpService.Status()
  } catch {
    // Nothing to draw but "off", which is what the state starts as.
  }
}

/** Ends one agent's sessions. The token stays: this is about the client speaking now. */
async function disconnectAgent(id: string): Promise<void> {
  try {
    await McpService.Disconnect(id)
  } catch {
    // The row is redrawn from the next status, which the server publishes either way.
  }
}

/** The window's decision on a send that is waiting for it. The question is cleared here: it is
    answered once, and a sheet that stayed open would be a second answer to nothing. */
function answerSend(allow: boolean): void {
  const ask = question.value
  question.value = null
  if (!ask) return
  void McpService.Answer(ask.id, allow)
}
