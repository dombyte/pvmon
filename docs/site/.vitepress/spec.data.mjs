// A VitePress data loader: gives the document to the browser code.
import { loadSpec } from './spec.mjs'

export default { watch: ['../../openapi.yaml'], load: loadSpec }
