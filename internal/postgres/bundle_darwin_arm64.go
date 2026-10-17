//go:build postgres

package postgres

import _ "embed"

//go:embed bundle/darwin_arm64.tar.gz
var bundle []byte
