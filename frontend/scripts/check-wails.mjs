import assert from 'node:assert/strict'
import { execFileSync } from 'node:child_process'
import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const frontendDir = dirname(dirname(fileURLToPath(import.meta.url)))
const projectDir = dirname(frontendDir)

/** @param {string} path */
function readJson(path) {
  return JSON.parse(readFileSync(path, 'utf8'))
}

const wails = JSON.parse(
  execFileSync(
    'go',
    ['mod', 'download', '-json', 'github.com/wailsapp/wails/v3'],
    {
      cwd: projectDir,
      encoding: 'utf8',
    },
  ),
)

assert.ok(wails.Dir, 'Wails module directory was not resolved')

const runtimePackage = readJson(
  join(
    wails.Dir,
    'internal/runtime/desktop/@wailsio/runtime/package.json',
  ),
)

const packageJson = readJson(join(frontendDir, 'package.json'))
const packageLock = readJson(join(frontendDir, 'package-lock.json'))

const expected = runtimePackage.version
const declared = packageJson.dependencies?.['@wailsio/runtime']
const locked =
  packageLock.packages?.['node_modules/@wailsio/runtime']?.version

assert.equal(
  declared,
  expected,
  `package.json must pin @wailsio/runtime to ${expected}`,
)

assert.equal(
  locked,
  expected,
  `package-lock.json must lock @wailsio/runtime to ${expected}`,
)

process.stdout.write(
  `Wails runtime matches Go distribution: ${expected}\n`,
)
