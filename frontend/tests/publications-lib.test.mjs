import assert from 'node:assert/strict'
import { test } from 'node:test'
import { vite } from './vite.mjs'

const load = await vite({
  '/src/i18n/index.ts': `export const t = (key) => key; export const te = () => true;
    export const currentLocale = () => 'en'`,
})
const { visibilityLabel, pageName } = await load('/src/lib/publicationText.ts')

test('each access is named by the catalogue, and an unknown one reads as unlisted', () => {
  assert.equal(visibilityLabel('public'), 'publications.visPublic')
  assert.equal(visibilityLabel('password'), 'publications.visPassword')
  assert.equal(visibilityLabel('unlisted'), 'publications.visUnlisted')
  assert.equal(visibilityLabel(undefined), 'publications.visUnlisted')
  assert.equal(visibilityLabel('somebody-elses'), 'publications.visUnlisted')
})

test('a page is called by its collection, then by its slug, then by nothing', () => {
  const tree = [{ id: 'outer', name: 'Shop', children: [{ id: 'inner', name: 'Orders', children: [] }] }]
  assert.equal(pageName(tree, 'outer', 'shop-1'), 'Shop')
  assert.equal(pageName(tree, 'inner', 'orders-1'), 'Orders')
  assert.equal(pageName(tree, 'gone', 'shop-1'), 'shop-1')
  assert.equal(pageName(tree, 'gone'), '')
})
