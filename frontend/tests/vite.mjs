import { readFileSync } from 'node:fs'
import { after } from 'node:test'
import { compileScript, parse } from '@vue/compiler-sfc'
import ts from 'typescript'
import { createServer } from 'vite'
import vuePlugin from '@vitejs/plugin-vue'

/**
 * Starts Vite for one test file and returns a loader for the app's modules.
 *
 * `stubs` replaces modules with source written in the test:
 *   '/src/i18n/index.ts': 'export const t = key => key'   a key starting with / matches the end of a path
 *   'reka-ui': 'export const PopoverRoot = ...'           any other key is a package import
 *   '@milkdown/kit/': '...'                               a package key ending in / stands for every import under it
 * `other(path)` may answer for paths a plain key cannot name; returning undefined leaves the module as is.
 * `vue: true` compiles real .vue files; `inline` lists packages to bundle rather than import from Node.
 */
export async function vite(stubs = {}, { other, vue = false, inline = [] } = {}) {
  const packages = Object.keys(stubs).filter((key) => !key.startsWith('/'))
  const packageOf = (id) => packages.find((key) => id === key || (key.endsWith('/') && id.startsWith(key)))
  const server = await createServer({
    configFile: false,
    logLevel: 'error',
    ssr: { noExternal: [...packages.map((key) => key.replace(/\/$/, '')), ...inline] },
    optimizeDeps: { noDiscovery: true, include: [] },
    server: { middlewareMode: true, watch: null, hmr: false, ws: false },
    appType: 'custom',
    plugins: [
      ...(vue ? [vuePlugin()] : []),
      {
        name: 'test-stubs',
        enforce: 'pre',
        resolveId(id) {
          const key = packageOf(id)
          if (key) return '\0stub:' + key
        },
        load(id) {
          if (id.startsWith('\0stub:')) return stubs[id.slice('\0stub:'.length)]
          const path = id.replaceAll('\\', '/')
          for (const [key, source] of Object.entries(stubs)) {
            if (key.startsWith('/') && path.endsWith(key)) return source
          }
          return other?.(path)
        },
      },
    ],
  })
  after(() => server.close())
  return (path) => server.ssrLoadModule(path)
}

/**
 * A component compiled for the browser rather than for server rendering, so a test can draw it with
 * its own renderer. Return it from `other` for the component under test.
 */
export function clientComponent(path) {
  const { descriptor } = parse(readFileSync(path, 'utf8'), { filename: path })
  const script = compileScript(descriptor, { id: path, inlineTemplate: true })
  return ts.transpileModule(script.content, {
    compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2022 },
  }).outputText
}
