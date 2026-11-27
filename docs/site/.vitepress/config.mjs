import { fileURLToPath } from 'node:url'
import { defineConfig } from 'vitepress'
import { apiSidebar } from './spec.mjs'

const repo = 'https://github.com/dombyte/pvmon'
const version = process.env.VITE_APP_VERSION || 'dev'
const page = (text, link) => ({ text, link })

export default defineConfig({
  title: 'pvmon',
  description: 'Self-hosted monitor for photovoltaic systems',
  lang: 'en',
  base: '/pvmon/', // GitHub Pages serves the repository under this path
  outDir: '../dist-site',
  // openapi.yaml is outside of the site, so the dev server must watch it explicitly.
  vite: {
    plugins: [
      {
        name: 'watch-openapi',
        configureServer: (server) =>
          void server.watcher.add(fileURLToPath(new URL('../../openapi.yaml', import.meta.url))),
      },
    ],
  },
  transformPageData(page) {
    if (page.params?.title) page.title = page.params.title
  },
  themeConfig: {
    search: { provider: 'local' },
    socialLinks: [{ icon: 'github', link: repo }],
    editLink: { pattern: `${repo}/edit/main/docs/site/:path` },
    footer: { message: `Version ${version}. MIT license.` },
    nav: [
      page('Guide', '/getting-started/install'),
      page('Reference', '/reference/configuration'),
      page('API', '/api/'),
    ],
    sidebar: {
      '/api/': apiSidebar(),
      '/': [
        {
          text: 'Getting started',
          items: [
            page('Installation', '/getting-started/install'),
            page('First start', '/getting-started/first-start'),
          ],
        },
        {
          text: 'Concepts',
          items: [
            page('Site model', '/concepts/site-model'),
            page('Energy and data quality', '/concepts/energy'),
            page('Grid netting', '/concepts/netting'),
          ],
        },
        {
          text: 'Guides',
          items: [
            page('Configuration', '/guides/configuration'),
            page('Example setups', '/guides/setups'),
            page('The web interface', '/guides/web-interface'),
            page('Security', '/guides/security'),
            page('Backups and operation', '/guides/operation'),
            page('Import from solis', '/guides/import-solis'),
            page('Write a device profile', '/guides/write-a-profile'),
          ],
        },
        {
          text: 'Reference',
          items: [
            page('Configuration keys', '/reference/configuration'),
            page('Command line', '/reference/cli'),
            page('Supported devices', '/reference/devices'),
            page('Profile schema', '/reference/profiles'),
            page('WebSocket', '/reference/websocket'),
            page('REST API', '/api/'),
          ],
        },
        { text: 'Help', items: [page('Troubleshooting', '/help/faq')] },
      ],
    },
  },
})
