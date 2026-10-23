package controlplane

import (
	"crypto/x509"
	"testing"
	"time"

	"komrad/internal/controlplanetest"
	"komrad/internal/forge/github"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTheForgeProxyMintsTheNextCertificateBeforeItsLastRunsOut covers the certificate for
// GitHub's names kept while it has a week left and replaced once it does not.
func TestTheForgeProxyMintsTheNextCertificateBeforeItsLastRunsOut(t *testing.T) {
	ca := controlplanetest.NewCA(t)
	at := time.Now()
	signer := newLeafSigner(ca.Cert, func() time.Time { return at })

	first, err := signer.getCertificate(nil)
	require.NoError(t, err)

	for _, host := range github.ForgeProxyHosts {
		_, err := first.Leaf.Verify(x509.VerifyOptions{DNSName: host, Roots: ca.Pool, CurrentTime: at})
		require.NoError(t, err, host)
	}

	at = at.Add(leafLifetime - leafRenewal - time.Hour)
	kept, err := signer.getCertificate(nil)
	require.NoError(t, err)
	assert.Same(t, first, kept)

	at = at.Add(2 * time.Hour)
	next, err := signer.getCertificate(nil)
	require.NoError(t, err)
	assert.NotSame(t, first, next)
	assert.True(t, next.Leaf.NotAfter.After(first.Leaf.NotAfter))
}
