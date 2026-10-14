import { useCollectionsStore } from '../../stores/collections'
import { useEnvironmentsStore } from '../../stores/environments'
import { useRecordsStore } from '../../stores/records'
import { useTabsStore } from '../../stores/tabs'
import { useWorkspacesStore } from '../../stores/workspaces'
import { useCookiesStore } from '../../stores/cookies'
import { usePublicationsStore } from '../../stores/publications'
import { reloadScripts } from '../../stores/content'

export async function switchWorkspace(id: string): Promise<boolean> {
  const workspaces = useWorkspacesStore()
  const records = useRecordsStore()
  const collections = useCollectionsStore()

  if (id === workspaces.activeId) return true

  if (!(await collections.askUnsaved())) return false

  // Every tab's owed keystrokes go home before the space changes: the drafts are the space's.
  await records.flush()

  try {
    await workspaces.switch(id)
  } catch {
    return false
  }

  await enterWorkspace()
  return true
}

export async function enterWorkspace(): Promise<void> {
  const records = useRecordsStore()
  const collections = useCollectionsStore()
  const tabs = useTabsStore()
  const environments = useEnvironmentsStore()
  const cookies = useCookiesStore()
  const publications = usePublicationsStore()

  records.reset()
  collections.reset()
  tabs.reset()
  environments.reset()
  publications.reset()
  await cookies.reset()

  const strip = tabs.load()

  await Promise.all([
    strip.then(() => reloadScripts()),
    records.load(),
    collections.load(),
    environments.load(),
    publications.load(),
  ])

  await environments.ensureDefaults()
}
