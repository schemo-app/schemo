import type { MessageKey } from '../i18n'
import type { CategoryId } from '../components/settings/categories'

// What the search looks through: one entry per row the window can draw, written out because a pane
// is not on the screen until its section is opened. `[section, group, row, words]`.
type Entry = [CategoryId, MessageKey, MessageKey, MessageKey]

const INDEX: readonly Entry[] = [
  // Account. Its groups are the ones the pane draws, and they live in the account's own catalogue.
  ['account', 'account.sync', 'sync.thisDevice', 'settings.find.sync'],
  ['account', 'account.sync', 'account.syncRuns', 'settings.find.syncRuns'],
  ['account', 'account.sync', 'account.syncSecrets', 'settings.find.syncSecrets'],
  ['account', 'account.plan', 'account.planCta', 'settings.find.plan'],
  ['account', 'account.entry', 'account.otherDevices', 'settings.find.devices'],
  ['account', 'account.entry', 'account.signOut', 'settings.find.signOut'],
  ['account', 'settings.sec.account', 'account.panel', 'settings.find.panel'],

  // General.
  ['general', 'settings.sec.startup', 'settings.reopenWorkspace', 'settings.find.reopenWorkspace'],
  ['general', 'settings.sec.startup', 'settings.restoreTabs', 'settings.find.restoreTabs'],
  ['general', 'settings.sec.editor', 'settings.wrapLines', 'settings.find.wrapLines'],
  ['general', 'settings.sec.editor', 'settings.lineNumbers', 'settings.find.lineNumbers'],
  ['general', 'settings.sec.editor', 'settings.indent', 'settings.find.indent'],
  // The language row is the editor section's last one, the way the drawing has it.
  ['general', 'settings.sec.editor', 'settings.interfaceLanguage', 'settings.find.language'],
  ['general', 'settings.sec.certificates', 'settings.certificates.verify', 'settings.find.verify'],
  [
    'general',
    'settings.sec.certificates',
    'settings.certificates.trustLocal',
    'settings.find.trustLocal',
  ],
  ['general', 'settings.sec.certificates', 'settings.certificates.roots', 'settings.find.roots'],

  // Appearance. The theme's row is the three cards together, named by the section it heads.
  ['appearance', 'settings.sec.theme', 'theme.title', 'settings.find.theme'],
  ['appearance', 'settings.sec.theme', 'settings.accent', 'settings.find.accent'],
  ['appearance', 'settings.sec.fonts', 'settings.uiSize', 'settings.find.uiSize'],
  ['appearance', 'settings.sec.fonts', 'settings.monoFont', 'settings.find.monoFont'],

  // The keyboard. Every command is a row, and they all answer to the same words: somebody looking
  // for a shortcut is not looking for a particular one.
  ['shortcuts', 'shortcuts.groups.general', 'shortcuts.cmd.palette', 'settings.find.shortcut'],
  ['shortcuts', 'shortcuts.groups.general', 'shortcuts.cmd.settings', 'settings.find.shortcut'],
  ['shortcuts', 'shortcuts.groups.general', 'shortcuts.cmd.sidebar', 'settings.find.shortcut'],
  ['shortcuts', 'shortcuts.groups.general', 'shortcuts.cmd.updates', 'settings.find.shortcut'],
  ['shortcuts', 'shortcuts.groups.request', 'shortcuts.cmd.send', 'settings.find.shortcut'],
  ['shortcuts', 'shortcuts.groups.request', 'shortcuts.cmd.save', 'settings.find.shortcut'],
  ['shortcuts', 'shortcuts.groups.request', 'shortcuts.cmd.environments', 'settings.find.shortcut'],
  ['shortcuts', 'shortcuts.groups.request', 'shortcuts.cmd.newRequest', 'settings.find.shortcut'],
  ['shortcuts', 'shortcuts.groups.request', 'shortcuts.cmd.duplicate', 'settings.find.shortcut'],
  ['shortcuts', 'shortcuts.groups.collections', 'shortcuts.cmd.run', 'settings.find.shortcut'],
  ['shortcuts', 'shortcuts.groups.response', 'shortcuts.cmd.find', 'settings.find.shortcut'],
  ['shortcuts', 'shortcuts.groups.response', 'shortcuts.cmd.inspector', 'settings.find.shortcut'],

  // Requests.
  ['requests', 'settings.sec.send', 'settings.timeout', 'settings.find.timeout'],
  ['requests', 'settings.sec.send', 'settings.redirects', 'settings.find.redirects'],
  ['requests', 'settings.sec.send', 'settings.cookies', 'settings.find.cookies'],
  ['requests', 'settings.sec.send', 'settings.httpVersion', 'settings.find.httpVersion'],
  ['requests', 'settings.sec.headers', 'settings.userAgent', 'settings.find.userAgent'],
  ['requests', 'settings.sec.headers', 'settings.acceptEncoding', 'settings.find.acceptEncoding'],
  ['requests', 'settings.sec.headers', 'settings.showAuto', 'settings.find.showAuto'],
  ['requests', 'settings.sec.history', 'settings.retention', 'settings.find.retention'],
  ['requests', 'settings.sec.history', 'settings.clearHistory', 'settings.find.clearHistory'],

  // The extension. Its rows are the ones the pane draws: the two switches, and what is let through.
  ['extension', 'extension.connection', 'extension.autoConnect', 'settings.find.autoConnect'],
  ['extension', 'extension.connection', 'extension.rememberTabs', 'settings.find.rememberTabs'],
  ['extension', 'extension.filters', 'extension.hosts', 'settings.find.hosts'],
  ['extension', 'extension.filters', 'extension.skipStatic', 'settings.find.skipStatic'],
  ['extension', 'extension.filters', 'extension.skipAnalytics', 'settings.find.skipAnalytics'],
  ['extension', 'extension.filters', 'extension.onlyJson', 'settings.find.onlyJson'],

  // The MCP server. Its groups are the pane's sections, the switch at the top stands for the
  // server itself, and the two rows that are only about it as a thing — the address and the
  // token — are the ones an agent's setup sends somebody looking for.
  ['mcp', 'settings.cat.mcp', 'mcp.toggleTitle', 'settings.find.mcp'],
  ['mcp', 'mcp.connection', 'mcp.port', 'settings.find.mcpPort'],
  ['mcp', 'mcp.connection', 'mcp.token', 'settings.find.mcpToken'],
  ['mcp', 'mcp.connection', 'mcp.agentConfig', 'settings.find.mcp'],
  ['mcp', 'mcp.allowed', 'mcp.mode', 'settings.find.mcpMode'],
  ['mcp', 'mcp.allowed', 'mcp.ask', 'settings.find.mcpAsk'],
  ['mcp', 'mcp.allowed', 'mcp.spaces', 'settings.find.mcpSpaces'],
  ['mcp', 'mcp.allowed', 'mcp.secrets', 'settings.find.mcpSecrets'],
  ['mcp', 'mcp.agents', 'mcp.agents', 'settings.find.mcpAgents'],

  // Proxy.
  ['proxy', 'settings.proxy.modeGroup', 'settings.proxy.mode', 'settings.find.proxyMode'],
  ['proxy', 'settings.proxy.environment', 'settings.proxy.environment', 'settings.find.proxyEnv'],
  ['proxy', 'settings.proxy.server', 'settings.proxy.address', 'settings.find.proxyHost'],
  ['proxy', 'settings.proxy.server', 'settings.proxy.auth', 'settings.find.proxyAuth'],
  ['proxy', 'settings.proxy.server', 'settings.proxy.bypass', 'settings.find.proxyBypass'],
  ['proxy', 'settings.proxy.server', 'settings.proxy.probe', 'settings.find.proxyProbe'],

  // Updates.
  ['updates', 'settings.sec.version', 'update.check', 'settings.find.checkUpdates'],
  ['updates', 'settings.sec.version', 'settings.whatsNew', 'settings.find.whatsNew'],
  ['updates', 'settings.sec.channel', 'settings.channel', 'settings.find.channel'],
  ['updates', 'settings.sec.channel', 'settings.checkAutomatically', 'settings.find.autoCheck'],
  ['updates', 'settings.sec.channel', 'settings.autoDownload', 'settings.find.autoDownload'],
]

/** One row of the search answer, with the part of the title the query hit, cut in three to draw. */
export interface SettingsHit {
  tab: CategoryId
  group: string
  title: string
  before: string
  match: string
  after: string
}

// Every word of the entry is searched, title and keywords alike: someone looking for "timeout" in a
// Russian interface is not looking for a word the title has.
function matches(t: (key: MessageKey) => string, entry: Entry, query: string): boolean {
  const [tab, group, title, words] = entry
  return [t(group), t(title), t(words), tab].join(' ').toLowerCase().includes(query)
}

/** Rows a query answers with, in the order the index declares them; an empty query finds none. */
export function settingsHits(t: (key: MessageKey) => string, raw: string): SettingsHit[] {
  const query = raw.trim().toLowerCase()
  if (!query) return []

  return INDEX.filter((entry) => matches(t, entry, query)).map(([tab, group, title]) => {
    const said = t(title)
    const at = said.toLowerCase().indexOf(query)
    return {
      tab,
      group: t(group),
      title: said,
      before: at >= 0 ? said.slice(0, at) : said,
      match: at >= 0 ? said.slice(at, at + query.length) : '',
      after: at >= 0 ? said.slice(at + query.length) : '',
    }
  })
}
