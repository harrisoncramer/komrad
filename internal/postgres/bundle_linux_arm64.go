//go:build postgres

package postgres

import _ "embed"

//go:embed bundle/linux_arm64.tar.gz
var bundle []byte
