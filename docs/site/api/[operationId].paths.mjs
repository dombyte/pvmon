import { loadSpec } from '../.vitepress/spec.mjs'

export default {
  watch: ['../../openapi.yaml'],
  paths: () =>
    Object.values(loadSpec().paths)
      .flatMap((path) => Object.values(path))
      .filter((operation) => operation.operationId)
      .map(({ operationId, summary }) => ({ params: { operationId, title: summary } })),
}
