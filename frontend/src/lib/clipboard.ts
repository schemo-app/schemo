import { Clipboard } from '@wailsio/runtime'

export async function copyToClipboard(text: string): Promise<boolean> {
  try {
    await Clipboard.SetText(text)
    return true
  } catch {
    return false
  }
}
