import { ref } from 'vue'

/** A row somebody is filling in: an address, and the role it will be given. */
export interface InviteDraft {
  key: number
  email: string
  roleId: string
}

/** Invitation rows kept locally, since the workspace they are for may not exist yet. */
export function useInviteDrafts() {
  const drafts = ref<InviteDraft[]>([])
  let next = 1

  function addDraft() {
    drafts.value = [...drafts.value, { key: next++, email: '', roleId: '' }]
  }

  function dropDraft(key: number) {
    drafts.value = drafts.value.filter((one) => one.key !== key)
  }

  function clearDrafts() {
    drafts.value = []
  }

  return { drafts, addDraft, dropDraft, clearDrafts }
}
