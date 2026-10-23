import { PANEL } from './layout'

/** The inspector as a pane keeps it: whether it is open, and the width it was dragged to. */
export interface InspectorState {
  open: boolean
  width: number
}

/** The inspector nothing has touched yet: closed, at its default width. */
export function freshInspector(): InspectorState {
  return { open: false, width: PANEL.default }
}
