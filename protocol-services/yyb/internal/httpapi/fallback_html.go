package httpapi

import "yyb_go/resource"

// Data directories need not contain UI files. Always fall back to the complete,
// embedded console rather than a reduced placeholder page.
var fallbackIndexHTML = resource.IndexHTML
var fallbackScanHTML = resource.ScanHTML

const docsHTML = `<!doctype html><html lang="zh-CN"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>YYB Go API 文档</title>
<body style="margin:32px;font-family:system-ui,-apple-system,BlinkMacSystemFont,'Segoe UI','Microsoft YaHei',sans-serif;line-height:1.6">
<h1>YYB Go API</h1>
<p>Swagger UI: <a href="/docs/index.html">/docs/index.html</a></p>
<p>OpenAPI JSON: <a href="/openapi.json">/openapi.json</a></p>
</body></html>`

var openAPISpec = newOpenAPISpec()
