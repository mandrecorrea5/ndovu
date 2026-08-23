// Package api embeda a especificação OpenAPI escrita à mão,
// servida pela própria API em /openapi.yaml e /docs.
package api

import "embed"

// SpecFS contém o openapi.yaml.
//
//go:embed openapi.yaml
var SpecFS embed.FS
