import { t } from '../i18n'

/** One row of a sheet: what it is about, and — on a sheet that asks first — what becomes of it. */
export interface SheetLine {
  label: string
  note?: string
  tag?: string
  tone?: 'keep' | 'drop'
}

/** The rows of a sheet that asks before it deletes: what goes with the action and what stays. */
export function useSheetLines() {

  return {
    /** Something the action takes away. The tag says "Deleted" unless the sheet has a word of its own. */
    gone: (label: string, note?: string, tag = t('common.deleted')): SheetLine => ({
      label,
      note,
      tag,
      tone: 'drop',
    }),
    /** Something the action leaves where it is. */
    kept: (label: string, note?: string): SheetLine => ({
      label,
      note,
      tag: t('common.kept'),
      tone: 'keep',
    }),
  }
}
