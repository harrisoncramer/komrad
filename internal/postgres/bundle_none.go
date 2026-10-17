//go:build !postgres || !((darwin || linux) && (arm64 || amd64))

package postgres

var bundle []byte
