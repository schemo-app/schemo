import { useWorkspacesStore } from '../stores/workspaces'
import { t } from '../i18n'
import type { Workspace } from '../../bindings/schemo/internal/domain'

export interface WorkspaceContentCount {
  value: number
  label: string
}

/** Composable providing workspace entity count formatting for switcher rows and manager views. */
export function useWorkspaceCounts() {
  const workspaces = useWorkspacesStore()

  /** Formats concise summary string of workspace contents (e.g. "2 collections · 1 environment"). */
  function metaOf(workspace: Workspace): string {
    const held = workspaces.countsOf(workspace.id)
    const parts: string[] = []
    if (held.collections > 0) parts.push(t('counts.collections', held.collections))
    if (held.environments > 0) parts.push(t('counts.environments', held.environments))
    return parts.length ? parts.join(' · ') : t('workspaces.empty')
  }

  /** Returns detailed array of workspace item counts with localized labels. */
  function contentsOf(workspace: Workspace): WorkspaceContentCount[] {
    const held = workspaces.countsOf(workspace.id)
    return [
      { value: held.collections, label: t('workspaces.contents.collections', held.collections) },
      { value: held.environments, label: t('workspaces.contents.environments', held.environments) },
      { value: held.runs, label: t('workspaces.contents.runs', held.runs) },
    ]
  }

  return { metaOf, contentsOf }
}