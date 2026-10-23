import { readFileSync, readdirSync } from 'node:fs'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

export function messagesOf(value, path = '', messages = new Map()) {
  if (typeof value === 'string') {
    if (messages.has(path)) throw new Error(`Duplicate message: ${path}`)
    messages.set(path, value)
  } else if (value && typeof value === 'object' && !Array.isArray(value)) {
    for (const [key, message] of Object.entries(value)) {
      messagesOf(message, path ? `${path}.${key}` : key, messages)
    }
  } else {
    throw new Error(`${path}: expected a string or message group`)
  }
  return messages
}

// The panel's syntax (app/i18n.ts): `{name}` parameters and plural forms separated by `|`. Any
// other brace is a typo that would be shown to the person as is.
function parametersOf(message, report) {
  const parameters = new Set()
  const stray = message.replace(/\{(\w+)\}/g, (_, name) => {
    parameters.add(`{${name}}`)
    return ''
  })
  if (/[{}]/.test(stray)) report('unbalanced brace; parameters are written {name}')
  const forms = message.split('|').map(form => form.trim())
  if (forms.length > 1 && forms.some(form => !form)) report('empty plural form')
  return { parameters: [...parameters].sort().join(', '), plural: forms.length > 1 }
}

export function checkTranslations(languages, catalogues) {
  const errors = []
  if (!catalogues.en) return ['en: missing reference catalogue']
  const reference = messagesOf(catalogues.en)
  const referenceMessages = new Map([...reference].map(([key, message]) => [
    key, parametersOf(message, error => errors.push(`en:${key}: ${error}`)),
  ]))
  if (!languages.en) errors.push('languages.json must include en')
  for (const [code, name] of Object.entries(languages)) {
    try {
      if (Intl.getCanonicalLocales(code)[0] !== code || code === 'system') throw new Error()
    } catch {
      errors.push(`languages.json: invalid canonical language tag ${code}`)
    }
    if (typeof name !== 'string' || !name.trim()) errors.push(`${code}: missing native language name`)
    if (!catalogues[code]) errors.push(`${code}: missing translation file`)
  }
  for (const [code, catalogue] of Object.entries(catalogues)) {
    if (!Object.hasOwn(languages, code)) errors.push(`${code}: missing languages.json entry`)
    const messages = messagesOf(catalogue)
    for (const key of reference.keys()) if (!messages.has(key)) errors.push(`${code}:${key}: missing message`)
    for (const [key, message] of messages) {
      if (!reference.has(key)) errors.push(`${code}:${key}: unknown message`)
      if (reference.get(key)?.trim() && !message.trim()) errors.push(`${code}:${key}: empty message; keep English until translated`)
      const parsed = code === 'en'
        ? referenceMessages.get(key)
        : parametersOf(message, error => errors.push(`${code}:${key}: ${error}`))
      if (code !== 'en' && parsed.plural !== referenceMessages.get(key)?.plural) {
        errors.push(`${code}:${key}: plural forms must match English: both counted or neither`)
      }
      const expected = referenceMessages.get(key)?.parameters
      if (code !== 'en' && parsed.parameters !== expected) {
        errors.push(`${code}:${key}: expected parameters ${expected || '(none)'}, got ${parsed.parameters || '(none)'}`)
      }
    }
  }
  return errors
}

export function readTranslations() {
  const source = join(dirname(fileURLToPath(import.meta.url)), '../src/i18n')
  const languages = readJson(join(source, 'languages.json'))
  const catalogues = Object.fromEntries(readdirSync(join(source, 'locales'))
    .filter(file => file.endsWith('.json'))
    .sort()
    .map(file => [file.slice(0, -5), readJson(join(source, 'locales', file))]))
  return { languages, catalogues }
}

function readJson(path) {
  try {
    return JSON.parse(readFileSync(path, 'utf8'))
  } catch (error) {
    throw new Error(`${path}: ${error.message}`)
  }
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try {
    const { languages, catalogues } = readTranslations()
    const errors = checkTranslations(languages, catalogues)
    if (errors.length) throw new Error(errors.join('\n'))
    console.log(`Translations OK: ${Object.keys(languages).join(', ')}; ${messagesOf(catalogues.en).size} messages each`)
  } catch (error) {
    console.error(error.message)
    process.exitCode = 1
  }
}
