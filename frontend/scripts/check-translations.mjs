import { readFileSync, readdirSync } from 'node:fs'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { createParser } from '@intlify/message-compiler'

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

function parametersOf(message, report) {
  const parameters = new Set()
  const linked = new Set()
  const ast = createParser({ onError: error => report(error.message) }).parse(message)
  function visit(node) {
    if (!node || typeof node !== 'object') return
    if (typeof node.key === 'string') parameters.add(`{${node.key}}`)
    if (typeof node.index === 'number') parameters.add(`{${node.index}}`)
    if (typeof node.key?.value === 'string') linked.add(node.key.value)
    for (const value of Object.values(node)) {
      if (Array.isArray(value)) value.forEach(visit)
      else if (value && typeof value === 'object') visit(value)
    }
  }
  visit(ast)
  return { parameters: [...parameters].sort().join(', '), linked }
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
      for (const link of parsed.linked) {
        if (!reference.has(link)) errors.push(`${code}:${key}: unknown linked message ${link}`)
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
  const source = join(dirname(fileURLToPath(import.meta.url)), '../src')
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
