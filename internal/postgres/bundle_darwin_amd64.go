//go:build postgres

package postgres

import _ "embed"

//go:embed bundle/darwin_amd64.tar.gz
var bundle []byte
