// This is a separate file, because the Content-Security-Policy does not allow inline scripts.
window.SwaggerUIBundle({
  url: 'openapi.yaml',
  dom_id: '#swagger-ui',
  // The default validator sends a request to validator.swagger.io. pvmon must not do this.
  validatorUrl: null,
})
