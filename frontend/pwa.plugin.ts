// Builds the service worker (src/sw/sw.ts) as its own entry "sw.js" and fills in what it
// precaches: every emitted file plus the public files, and a build id hashed from their
// contents, so a new build is a new cache and an unchanged build keeps its cache.
import { createHash } from 'node:crypto'
import { readdirSync, readFileSync } from 'node:fs'
import { join } from 'node:path'
import type { Plugin } from 'vite'

const swEntry = 'sw'
const swFile = 'sw.js'
const buildIdLength = 12

function publicFiles(dir: string): string[] {
  try {
    return readdirSync(dir, { recursive: true, withFileTypes: true })
      .filter((e) => e.isFile() && !e.name.startsWith('.'))
      .map((e) => join(e.parentPath, e.name).slice(dir.length + 1).replaceAll('\\', '/'))
  } catch {
    return []
  }
}

export function pwa(): Plugin {
  let publicDir = ''
  return {
    name: 'pvmon-pwa',
    apply: 'build',
    // After vite's html plugin, which emits index.html in its own generateBundle.
    enforce: 'post',
    configResolved(c) {
      publicDir = c.publicDir
    },
    generateBundle(_, bundle) {
      const hash = createHash('sha256')
      const files: string[] = []
      for (const [name, item] of Object.entries(bundle).sort(([a], [b]) => a.localeCompare(b))) {
        if (name === swFile || name.endsWith('.map')) continue
        files.push(`/${name}`)
        hash.update(name).update(item.type === 'chunk' ? item.code : item.source)
      }
      for (const name of publicFiles(publicDir).sort()) {
        files.push(`/${name}`)
        hash.update(name).update(readFileSync(join(publicDir, name)))
      }
      const sw = bundle[swFile]
      if (sw?.type !== 'chunk') this.error('pvmon-pwa: sw.js entry missing')
      const build = hash.digest('hex').slice(0, buildIdLength)
      sw.code = sw.code
        .replaceAll('__PVMON_BUILD__', JSON.stringify(build))
        .replaceAll('__PVMON_PRECACHE__', JSON.stringify(files))
      // A classic worker script cannot import: a module shared with the app would break it.
      if (sw.imports.length > 0 || sw.dynamicImports.length > 0) {
        this.error(`pvmon-pwa: sw.js imports ${sw.imports.join(', ')}; keep src/sw separate`)
      }
    },
  }
}

/** Rollup inputs and file names: the app page plus the worker at /sw.js. */
export const pwaInput = { index: 'index.html', [swEntry]: 'src/sw/sw.ts' }
export const entryFileNames = (c: { name: string }) =>
  c.name === swEntry ? swFile : 'assets/[name]-[hash].js'
