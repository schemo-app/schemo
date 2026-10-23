import assert from 'node:assert/strict'
import { afterEach, test } from 'node:test'
import { readTranslations } from '../scripts/check-translations.mjs'
import { vite } from './vite.mjs'

const { catalogues } = readTranslations()

const load = await vite()
const { currentLocale, loadCatalogue, resolveLocale, setLocale, setMessages, t, te } = await load('/src/i18n/index.ts')
afterEach(() => {
  setLocale('en')
  setMessages('ru', catalogues.ru)
})

test('system language negotiation honors preference order and regional variants', () => {
  assert.equal(resolveLocale(['ru-RU', 'en-US']), 'ru')
  assert.equal(resolveLocale(['en-GB', 'ru']), 'en')
  assert.equal(resolveLocale(['de-DE', 'ru-RU']), 'ru')
  assert.equal(resolveLocale(['de-DE']), 'en')
  assert.equal(resolveLocale(['invalid!', 'ru-RU']), 'ru')
})

test('JSON catalogues are loaded lazily by locale code', async () => {
  setMessages('ru', undefined)
  setLocale('ru')
  assert.equal(t('common.save'), catalogues.en.common.save)
  await loadCatalogue('ru')
  assert.equal(currentLocale(), 'ru')
  assert.equal(t('common.save'), catalogues.ru.common.save)
})

test('English and Russian plural messages use Intl rules', async () => {
  assert.equal(t('counts.requests', { n: 0 }), '0 requests')
  assert.equal(t('counts.requests', { n: 1 }), '1 request')
  await loadCatalogue('ru')
  setLocale('ru')
  const forms = catalogues.ru.counts.requests.split('|').map(form => form.trim())
  for (const [count, index] of [[0, 2], [1, 0], [2, 1], [5, 2], [21, 0], [22, 1]]) {
    assert.equal(t('counts.requests', { n: count }), forms[index].replace('{n}', String(count)))
  }
})

test('missing translations and error keys fall back to English', () => {
  setMessages('ru', { common: { save: 'Localized save' } })
  setLocale('ru')
  assert.equal(t('common.save'), 'Localized save')
  assert.equal(t('common.copy'), 'Copy')
  assert.equal(te('common.copy'), true)
})


test('every refusal code Go sends has a sentence', async () => {
  const { Code } = await load('/bindings/schemo/internal/domain/models.ts')
  const codes = Object.values(Code).filter((code) => code !== '')
  assert.ok(codes.length > 10)
  for (const code of codes) assert.ok(catalogues.en.errors.codes[code], `errors.codes.${code} is missing`)
})
