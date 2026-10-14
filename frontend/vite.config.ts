import {defineConfig} from 'vite'
import vue from '@vitejs/plugin-vue'
import tailwindcss from '@tailwindcss/vite'
import wails from '@wailsio/runtime/plugins/vite'

const FAMILIES: [string, RegExp][] = [
  ['vendor-vue', /node_modules\/(vue|@vue|pinia|vue-i18n|@intlify|vue-demi)\//],
  ['vendor-ui', /node_modules\/(reka-ui|@floating-ui|@internationalized|@tanstack|@vueuse|aria-hidden|defu|ohash)\//],
  ['vendor-editor', /node_modules\/(@codemirror|@lezer|crelt|w3c-keyname|style-mod|@marijn)\//],
  ['vendor-markdown', /node_modules\/(marked|dompurify)\//],
  ['vendor-wysiwyg', /node_modules\/(@milkdown|@ocavue|prosemirror-[^/]+|orderedmap|rope-sequence|remark[^/]*|unified|mdast-util-[^/]+|micromark[^/]*|unist-util-[^/]+|vfile[^/]*|lodash-es|nanoid|debug|ms|bail|trough|zwitch|is-plain-obj|extend|decode-named-character-reference|character-entities|longest-streak|markdown-table|ccount|escape-string-regexp|devlop|dequal|clsx)\//],
  ['vendor-wails', /node_modules\/@wailsio\//]
]

function vendorChunk(id: string): string | undefined {
  if (!id.includes('node_modules')) return undefined

  return FAMILIES.find(([, family]) => family.test(id))?.[0] ?? 'vendor'
}

export default defineConfig({
  server: {
    host: '127.0.0.1',
    port: Number(process.env.WAILS_VITE_PORT) || 9245,
    strictPort: true
  },
  plugins: [vue(), tailwindcss(), wails('./bindings')],
  optimizeDeps: {
    exclude: ['@wailsio/runtime']
  },
  define: {
    __VUE_I18N_FULL_INSTALL__: true,
    __VUE_I18N_LEGACY_API__: false,
    __INTLIFY_PROD_DEVTOOLS__: false
  },
  build: {
    rollupOptions: {
      input: {
        main: 'index.html',
        about: 'about.html',
        settings: 'settings.html',
        update: 'update.html'
      },
      output: {manualChunks: vendorChunk}
    }
  }
})
