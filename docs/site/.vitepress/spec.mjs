// Reads docs/openapi.yaml for the API pages, with the values of the published site.
import { readFileSync } from 'node:fs'
import { parseSpec, useSidebar } from 'vitepress-openapi'

export function loadSpec() {
  const spec = parseSpec(readFileSync(new URL('../../openapi.yaml', import.meta.url), 'utf8'))
  if (!spec.paths) throw new Error('openapi.yaml: cannot read the file')
  spec.info.version = process.env.VITE_APP_VERSION || 'dev'
  // The site has no pvmon instance, so the examples use the host of the reader.
  spec.servers = [{ url: 'http://localhost:8080', description: 'Your pvmon instance' }]
  return spec
}

// One sidebar group for each tag, one page for each operation.
export function apiSidebar() {
  const sidebar = useSidebar({ spec: loadSpec(), linkPrefix: '/api/' })
  return [{ text: 'REST API', link: '/api/' }, ...sidebar.generateSidebarGroups()]
}
