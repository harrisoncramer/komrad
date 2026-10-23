package controlplane

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"fmt"
	"math/big"
	"os"
	"sync"
	"time"

	"komrad/internal/forge/github"
)

// leafLifetime is how long a certificate the forge proxy mints for GitHub's names lasts.
const leafLifetime = 30 * 24 * time.Hour

// leafRenewal is how long before it runs out the forge proxy mints the next one.
const leafRenewal = 7 * 24 * time.Hour

// LoadCA reads the CA certificate and its private key from PEM files.
func LoadCA(certFile, keyFile string) (*tls.Certificate, error) {
	certPEM, err := os.ReadFile(certFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read the CA certificate: %w", err)
	}

	keyPEM, err := os.ReadFile(keyFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read the CA's private key: %w", err)
	}

	ca, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, fmt.Errorf("failed to load the CA: %w", err)
	}
	if !ca.Leaf.IsCA {
		return nil, errors.New("the CA certificate is not a CA")
	}

	return &ca, nil
}

// leafSigner hands out the certificate for GitHub's names, minting a new one before it runs out.
type leafSigner struct {
	ca     *tls.Certificate
	now    func() time.Time
	mu     sync.Mutex
	issued *tls.Certificate
}

func newLeafSigner(ca *tls.Certificate, now func() time.Time) *leafSigner {
	return &leafSigner{ca: ca, now: now}
}

// getCertificate is the current certificate for GitHub's names.
func (s *leafSigner) getCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.issued != nil && s.now().Before(s.issued.Leaf.NotAfter.Add(-leafRenewal)) {
		return s.issued, nil
	}

	issued, err := s.mint()
	if err != nil {
		return nil, err
	}

	s.issued = issued

	return issued, nil
}

func (s *leafSigner) mint() (*tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to make a key for GitHub's names: %w", err)
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, fmt.Errorf("failed to make a serial number: %w", err)
	}

	now := s.now()
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "github.com"},
		DNSNames:     github.ForgeProxyHosts,
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.Add(leafLifetime),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}

	signer, ok := s.ca.PrivateKey.(crypto.Signer)
	if !ok {
		return nil, errors.New("the CA's private key cannot sign")
	}

	der, err := x509.CreateCertificate(rand.Reader, template, s.ca.Leaf, key.Public(), signer)
	if err != nil {
		return nil, fmt.Errorf("failed to sign the certificate for GitHub's names: %w", err)
	}

	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, fmt.Errorf("failed to read back the certificate for GitHub's names: %w", err)
	}

	return &tls.Certificate{
		Certificate: [][]byte{der, s.ca.Certificate[0]},
		PrivateKey:  key,
		Leaf:        leaf,
	}, nil
}
