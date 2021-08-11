package security

import (
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"fmt"

	certUtil "k8s.io/client-go/util/cert"

	p12 "software.sslmate.com/src/go-pkcs12"
)

func GenerateTruststore(pemFiles [][]byte, password string) ([]byte, error) {
	var certs []*x509.Certificate
	for _, pemFile := range pemFiles {
		pemRaw := pemFile
		for {
			block, rest := pem.Decode(pemRaw)
			if block == nil {
				break
			}

			if block.Type == certUtil.CertificateBlockType {
				cert, err := x509.ParseCertificate(block.Bytes)
				if err != nil {
					return nil, fmt.Errorf("Unable to parse certificate: %w", err)
				}
				certs = append(certs, cert)
			} else {
				return nil, fmt.Errorf("Unable to process pem entry type %s when generating truststore. Only CERTIFICATE is supported.", block.Type)
			}
			pemRaw = rest
		}
	}
	truststore, err := p12.EncodeTrustStore(rand.Reader, certs, password)
	if err != nil {
		return nil, fmt.Errorf("Unable to create truststore with user provided cert files: %w", err)
	}
	return truststore, nil
}
