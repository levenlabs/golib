package testutil

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"time"
)

// SignedCert generates a PEM encoded certificate/key pair for the given domain.
// If parentCertPEM/parentKeyPEM are given the certificate will be signed by
// that parent, otherwise it will be self-signed. If domain is not given, the
// returned certificate will itself be a signing CA certificate
//
// This function will panic if it receives invalid input or can't read from
// rand.Reader for some reason
func SignedCert(domain string, parentCertPEM, parentKeyPEM []byte) ([]byte, []byte) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(err)
	}
	keyDER := x509.MarshalPKCS1PrivateKey(key)

	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		panic(err)
	}

	now := time.Now()
	expire := now.AddDate(0, 3, 0)

	template := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"Test Inc."},
		},
		NotBefore:             now,
		NotAfter:              expire,
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	if domain == "" {
		template.IsCA = true
		template.KeyUsage |= x509.KeyUsageCertSign
	} else {
		template.DNSNames = []string{domain}
	}

	// start off assuming self signed, unless an actual parent is given
	parentCert := template
	parentKey := key

	if parentCertPEM != nil {
		p, _ := pem.Decode(parentCertPEM)
		if p == nil {
			panic("malformed parentCert PEM")
		}
		if parentCert, err = x509.ParseCertificate(p.Bytes); err != nil {
			panic(err)
		}

		p, _ = pem.Decode(parentKeyPEM)
		if p == nil {
			panic("malformed parentKey PEM")
		}
		if parentKey, err = x509.ParsePKCS1PrivateKey(p.Bytes); err != nil {
			panic(err)
		}
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, parentCert, &key.PublicKey, parentKey)
	if err != nil {
		panic(err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: keyDER})

	return certPEM, keyPEM
}
