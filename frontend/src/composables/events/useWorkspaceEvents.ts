import { useWorkspacesStore } from '../../stores/workspaces'
import { useWailsEvent } from './useWailsEvent'

export function useWorkspaceEvents(): void {
  useWailsEvent('workspace:changed', () => void useWorkspacesStore().load())
}
