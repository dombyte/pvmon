// vitepress-openapi contains all syntax grammars of Shiki. Some have the GPL license. The
// API pages need only these (java is a part of xml). All have the MIT license.
const grammars = ['source.java', 'source.json', 'source.shell', 'text.html.markdown', 'text.xml']
// A smaller number shows that the plugin changed and that the removal does not operate.
const minimumRemoved = 200

// A grammar is JSON text in the code, so its quotes can have a backslash.
const scopes = (code) => [...code.matchAll(/\\?"scopeName\\?":\s*\\?"([^"\\]+)/g)].map((m) => m[1])

/** Build plugin: replaces each grammar file that the site does not use with an empty one. */
export function removeUnusedGrammars() {
  const removed = new Set()
  return {
    name: 'pvmon-remove-grammars',
    apply: (_, env) => env.command === 'build' && !env.isSsrBuild,
    enforce: 'pre',
    transform(code, id) {
      if (!id.includes('/node_modules/vitepress-openapi/dist/')) return null
      const found = scopes(code)
      if (found.length === 0 || found.every((s) => grammars.includes(s))) return null
      removed.add(id)
      return { code: 'export default []', map: null }
    },
    generateBundle(_, bundle) {
      if (removed.size < minimumRemoved) {
        this.error(`grammars: removed only ${removed.size} files`)
      }
      for (const chunk of Object.values(bundle)) {
        const bad = scopes(chunk.code ?? '').find((s) => !grammars.includes(s))
        if (bad) this.error(`grammars: ${chunk.fileName} contains the grammar ${bad}`)
      }
    },
  }
}
