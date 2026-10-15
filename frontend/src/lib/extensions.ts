import browserIDs from '../../../internal/transport/extension/browser_ids.json'

export const EXTENSION_STORES = [
  { name: 'Chrome', url: `https://chromewebstore.google.com/detail/${browserIDs.chrome}` },
  { name: 'Firefox', url: 'https://addons.mozilla.org/firefox/addon/schemo-bridge/' },
  { name: 'Edge', url: `https://microsoftedge.microsoft.com/addons/detail/${browserIDs.edge}` },
] as const
