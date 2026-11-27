// Writes LICENSES/THIRD_PARTY_NOTICES.md. With --check, it only examines that the file is
// up to date. It stops if a license is not in the list. It needs the build of the frontend
// and the packages in docs/.
import { execFileSync } from 'node:child_process'
import { readFileSync, readdirSync, writeFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const root = join(dirname(fileURLToPath(import.meta.url)), '..')
const target = join(root, 'LICENSES', 'THIRD_PARTY_NOTICES.md')
const allowed = ['0BSD', 'Apache-2.0', 'BSD-2-Clause', 'BSD-3-Clause', 'ISC', 'MIT', 'OFL-1.1']
// The platforms of .goreleaser.yaml: some modules are only in the build of one platform.
const platforms = ['linux/amd64', 'linux/arm', 'linux/arm64', 'darwin/amd64', 'darwin/arm64']
const licenseName = /^(licen[sc]e|copying|notice|patents)([-._].*)?(?<!\.go)$/i

const read = (...path) => readFileSync(join(root, ...path), 'utf8')
const files = (dir, name = licenseName) =>
  readdirSync(dir, { withFileTypes: true })
    .filter((e) => e.isFile() && name.test(e.name))
    .map((e) => join(dir, e.name))
    .sort()
const textOf = (paths) => paths.map((p) => readFileSync(p, 'utf8').trim()).join('\n\n')
const run = (command, args, env) =>
  execFileSync(command, args, {
    cwd: root,
    encoding: 'utf8',
    env: { ...process.env, CGO_ENABLED: '0', ...env },
    stdio: ['ignore', 'pipe', 'pipe'],
  })

function goModules() {
  const tool = run('go', ['tool', '-n', 'go-licenses']).trim()
  const format =
    '{{with .Module}}{{if not .Main}}{{.Path}} {{.Version}}\t{{.Dir}}\t{{$.Dir}}{{end}}{{end}}'
  const modules = new Map() // "path version" -> { dirs, licenses }
  const found = [] // [package path, license] from go-licenses
  for (const platform of platforms) {
    const [GOOS, GOARCH] = platform.split('/')
    const env = { GOOS, GOARCH }
    const list = run('go', ['list', '-deps', '-f', format, './cmd'], env)
    for (const line of list.split('\n').filter(Boolean)) {
      let [module, dir, pkg] = line.split('\t')
      if (!modules.has(module)) modules.set(module, { dirs: new Set([dir]), licenses: new Set() })
      // A package directory can have its own license: examine each level up to the root.
      for (; pkg.length > dir.length; pkg = dirname(pkg)) modules.get(module).dirs.add(pkg)
    }
    const report = run(tool, ['report', './cmd', '--ignore', 'github.com/dombyte/pvmon'], env)
    for (const row of report.trim().split('\n')) {
      const [path, , license] = row.split(',')
      found.push([path, license])
    }
  }
  for (const [path, license] of found) {
    // The module of a package is the longest module path at the start of its path.
    const owner = [...modules.keys()]
      .filter((m) => `${path}/`.startsWith(`${m.split(' ')[0]}/`))
      .sort((a, b) => b.length - a.length)[0]
    modules.get(owner)?.licenses.add(license)
  }
  return [...modules].sort().map(([module, { dirs, licenses }]) => {
    const [name, version] = module.split(' ')
    const text = textOf([...dirs].sort().flatMap((d) => files(d)))
    return { name, version, licenses: [...licenses].sort(), text }
  })
}

function webPackages() {
  const bundled = JSON.parse(read('frontend', 'dist', '.vite', 'license.json')).map((p) => {
    // Vite lists the license only. The Apache license also requires the NOTICE file.
    const notices = textOf(files(join(root, 'frontend', 'node_modules', p.name), /^notice/i))
    const text = p.text && `${p.text.trim()}\n\n${notices}`
    return { name: p.name, version: p.version, licenses: [p.identifier], text }
  })
  // Vite does not list packages that supply only CSS. Swagger UI is not a part of Vite.
  const extra = ['frontend/node_modules/tailwindcss', 'docs/node_modules/swagger-ui-dist']
  return bundled.concat(
    extra.map((path) => {
      const p = JSON.parse(read(path, 'package.json'))
      const text = textOf(files(join(root, path), /^(license|notice)$|LICENSE\.txt$/i))
      return { name: p.name, version: p.version, licenses: [p.license], text }
    }),
  )
}

// Parts that no tool finds. Their texts are in this directory.
function fixedParts() {
  return [
    ['Go runtime and standard library', 'BSD-3-Clause', 'go.txt'],
    ['Inter font', 'OFL-1.1', 'inter-font.txt'],
    ['microcheck (/bin/httpcheck, container image only)', 'MIT', 'microcheck.txt'],
    ['musl C library (in /bin/httpcheck)', 'MIT', 'musl.txt'],
    ['Mozilla CA certificates (container image only)', 'MPL-2.0', 'ca-certificates.txt'],
  ].map(([name, license, file]) => ({
    name,
    version: '',
    fixed: true,
    licenses: [license],
    text: read('LICENSES', file),
  }))
}

function document() {
  const parts = [...goModules(), ...webPackages(), ...fixedParts()]
  for (const p of parts) {
    // The fixed parts are a decision of the project, for example the MPL certificate list.
    const bad = p.fixed ? undefined : p.licenses.find((l) => !allowed.includes(l))
    if (bad) throw new Error(`licenses: license ${bad} of ${p.name} is not allowed`)
    if (!p.licenses.length || !p.text?.trim()) {
      throw new Error(`licenses: no license for ${p.name}`)
    }
  }
  const title = (p) => `${p.name} ${p.version}`.trim()
  const rows = parts.map((p) => `| ${p.name} | ${p.version} | ${p.licenses.join(', ')} |`)
  const texts = parts.map(
    (p) =>
      `### ${title(p)}\n\n<details>\n<summary>License text</summary>\n\n` +
      `~~~~text\n${p.text.replaceAll('\r', '').trim()}\n~~~~\n\n</details>\n`,
  )
  return [
    '# Third-party notices',
    '',
    'pvmon contains the third-party software below. The license of pvmon is in',
    '[LICENSE](../LICENSE). Do not edit this file: `make license-update` writes it.',
    '',
    '| Component | Version | License |',
    '| --- | --- | --- |',
    ...rows,
    '',
    '## License texts',
    '',
    ...texts,
  ].join('\n')
}

const text = document()
if (!process.argv.includes('--check')) {
  writeFileSync(target, text)
} else if (readFileSync(target, 'utf8') !== text) {
  console.error('licenses: the notices file is not up to date: run `make license-update`')
  process.exit(1)
}
console.log('licenses: ok')
