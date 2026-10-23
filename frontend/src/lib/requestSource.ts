import { AuthType, type Auth } from '../../bindings/schemo/internal/domain'
import type { paneSource } from '../stores/content'

export type RequestSource = ReturnType<typeof paneSource>

export const NO_AUTH: Auth = { type: AuthType.AuthNone, fields: {} }

export type ChipName = 'params' | 'headers' | 'auth' | 'body' | 'scripts' | 'docs' | 'examples'

