// Makes dist/, the Swagger UI that is part of the binary.
import { copyFileSync, mkdirSync, readFileSync, readdirSync, rmSync, writeFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const root = join(dirname(fileURLToPath(import.meta.url)), '..')
const require = createRequire(import.meta.url)
const swagger = dirname(require.resolve('swagger-ui-dist/package.json'))
const vendor = [
  'swagger-ui.css',
  'swagger-ui-bundle.js',
  'swagger-ui-bundle.js.LICENSE.txt',
  'LICENSE',
  'NOTICE',
]

const out = join(root, 'dist')
mkdirSync(out, { recursive: true })
// Remove the old output. Keep .gitkeep, because go:embed needs it.
for (const f of readdirSync(out)) {
  if (f !== '.gitkeep') rmSync(join(out, f), { recursive: true })
}
for (const f of vendor) copyFileSync(join(swagger, f), join(out, f))
for (const f of readdirSync(join(root, 'swagger'))) {
  copyFileSync(join(root, 'swagger', f), join(out, f))
}

const spec = readFileSync(join(root, 'openapi.yaml'), 'utf8')
const placeholder = /^ {2}version: dev .*$/m
if (!placeholder.test(spec)) throw new Error('openapi.yaml: version placeholder not found')
const version = JSON.stringify(process.env.VITE_APP_VERSION || 'dev')
writeFileSync(join(out, 'openapi.yaml'), spec.replace(placeholder, `  version: ${version}`))
