import assert from 'node:assert/strict'
import { after, afterEach, test } from 'node:test'
import { createServer } from 'vite'
import { checkTranslations, readTranslations } from '../scripts/check-translations.mjs'

const { languages, catalogues } = readTranslations()

test('all registered translation files match the English reference', () => {
  assert.deepEqual(checkTranslations(languages, catalogues), [])
})

test('translation checks preserve parameters across plural forms and language order', () => {
  assert.deepEqual(checkTranslations({ en: 'English', ru: 'Russian' }, {
    ru: { request: '{n} one | {n} few | {n} many' },
    en: { request: '{n} request | {n} requests' },
  }), [])
})

test('translation checks reject missing keys and changed placeholders', () => {
  const errors = checkTranslations({ en: 'English', de: 'Deutsch' }, {
    en: { hello: 'Hello {name}', count: '{n} items' },
    de: { hello: 'Hallo {person}', extra: 'Extra' },
  })
  assert.ok(errors.some(error => error.includes('de:count: missing')))
  assert.ok(errors.some(error => error.includes('de:extra: unknown')))
  assert.ok(errors.some(error => error.includes('expected parameters {name}, got {person}')))
})

test('empty translations cannot hide labels that exist in English', () => {
  const errors = checkTranslations({ en: 'English', de: 'Deutsch' }, {
    en: { save: 'Save', optional: '' }, de: { save: '', optional: '' },
  })
  assert.equal(errors.length, 1)
  assert.ok(errors[0].startsWith('de:save: empty message'))
})

test('translation checks use the Vue I18n parser for syntax and linked messages', () => {
  const errors = checkTranslations({ en: 'English', de: 'Deutsch' }, {
    en: { hello: 'Hello {name}', copy: '@:hello' },
    de: { hello: 'Hallo {name', copy: '@:missing' },
  })
  assert.ok(errors.some(error => error.startsWith('de:hello:')))
  assert.ok(errors.some(error => error.includes('unknown linked message missing')))
})

test('translation checks require a canonical language code and both registration and a file', () => {
  const errors = checkTranslations({ en: 'English', 'en-us': 'English', fr: 'Français' }, {
    en: { hello: 'Hello' }, de: { hello: 'Hallo' },
  })
  assert.ok(errors.some(error => error.includes('invalid canonical language tag en-us')))
  assert.ok(errors.some(error => error.includes('fr: missing translation file')))
  assert.ok(errors.some(error => error.includes('de: missing languages.json entry')))
})

const vite = await createServer({
  configFile: false,
  optimizeDeps: { noDiscovery: true, include: [] },
  server: { middlewareMode: true, watch: null, hmr: false, ws: false },
  appType: 'custom',
})
after(() => vite.close())
const { i18n, loadCatalogue, resolveLocale, t, te } = await vite.ssrLoadModule('/src/i18n.ts')
afterEach(() => {
  i18n.global.locale.value = 'en'
  i18n.global.setLocaleMessage('ru', catalogues.ru)
})

test('system language negotiation honors preference order and regional variants', () => {
  assert.equal(resolveLocale(['ru-RU', 'en-US']), 'ru')
  assert.equal(resolveLocale(['en-GB', 'ru']), 'en')
  assert.equal(resolveLocale(['de-DE', 'ru-RU']), 'ru')
  assert.equal(resolveLocale(['de-DE']), 'en')
  assert.equal(resolveLocale(['invalid!', 'ru-RU']), 'ru')
})

test('JSON catalogues are loaded lazily by locale code', async () => {
  delete i18n.global.messages.value.ru
  assert.equal(i18n.global.availableLocales.includes('ru'), false)
  await loadCatalogue('ru')
  assert.equal(i18n.global.getLocaleMessage('ru').common.save, catalogues.ru.common.save)
})

test('English and Russian plural messages use Intl rules', async () => {
  assert.equal(t('counts.requests', { n: 0 }), '0 requests')
  assert.equal(t('counts.requests', { n: 1 }), '1 request')
  await loadCatalogue('ru')
  i18n.global.locale.value = 'ru'
  const forms = catalogues.ru.counts.requests.split('|').map(form => form.trim())
  for (const [count, index] of [[0, 2], [1, 0], [2, 1], [5, 2], [21, 0], [22, 1]]) {
    assert.equal(t('counts.requests', { n: count }), forms[index].replace('{n}', String(count)))
  }
})

test('missing translations and error keys fall back to English', () => {
  i18n.global.setLocaleMessage('ru', { common: { save: 'Localized save' } })
  i18n.global.locale.value = 'ru'
  assert.equal(t('common.save'), 'Localized save')
  assert.equal(t('common.copy'), 'Copy')
  assert.equal(te('common.copy'), true)
})
