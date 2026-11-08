// Package resource contains the console shipped with every YYB executable.
package resource

import _ "embed"

//go:embed templates/index.html
var IndexHTML string

//go:embed templates/scan.html
var ScanHTML string
