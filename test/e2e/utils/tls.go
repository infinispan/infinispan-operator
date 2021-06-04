package utils

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io/ioutil"
	"math/big"
	"os"
	"os/exec"
	"time"

	certUtil "k8s.io/client-go/util/cert"
	"k8s.io/client-go/util/keyutil"

	p12 "software.sslmate.com/src/go-pkcs12"
)

const (
	ServerName         = "server"
	KeystorePassword   = "secret"
	TruststorePassword = "secret"
	keyBits            = 2048
	tmpDir             = "/tmp/infinispan/operator/tls"
)

var serialNumber int64 = 1

type certHolder struct {
	privateKey *rsa.PrivateKey
	cert       *x509.Certificate
	certBytes  []byte
}

// Returns the public and private keys o
func CreateServerCertificates() (publicKey, privateKey []byte, clientTLSConf *tls.Config) {
	ca := ca()
	server := cert(ServerName, ca)
	publicKey = server.getCertPEM()
	privateKey = server.getPrivateKeyPEM()

	certpool := x509.NewCertPool()
	certpool.AddCert(ca.cert)
	clientTLSConf = &tls.Config{
		RootCAs:    certpool,
		ServerName: ServerName,
	}
	return
}

// Returns a keystore using a self-signed certificate, and the corresponding tls.Config required by clients to connect to the server
func CreateKeystore() (keystore []byte, clientTLSConf *tls.Config) {
	ca := ca()
	server := cert(ServerName, ca)
	keystore = createKeystore(ca, server)

	certpool := x509.NewCertPool()
	certpool.AddCert(ca.cert)
	clientTLSConf = &tls.Config{
		RootCAs:    certpool,
		ServerName: ServerName,
	}
	return
}

func CreateKeystoreAndClientCerts() (keystore []byte, caPem []byte, clientPem []byte, clientTLSConf *tls.Config) {
	ca := ca()
	server := cert("server", ca)
	keystore = createKeystore(ca, server)
	client := cert("client", ca)

	certpool := x509.NewCertPool()
	certpool.AddCert(ca.cert)

	caPem = ca.getCertPEM()
	clientPem = client.getCertPEM()
	clientTLSConf = &tls.Config{
		GetClientCertificate: func(t *tls.CertificateRequestInfo) (*tls.Certificate, error) {
			certificate, err := tls.X509KeyPair(client.getCertPEM(), client.getPrivateKeyPEM())
			return &certificate, err
		},
		RootCAs:    certpool,
		ServerName: ServerName,
	}
	return
}

// Returns a keystore & truststore using a self-signed certificate, and the corresponding tls.Config required by clients to connect to the server
// If authenticate is true, then the returned truststore contains all client certificates, otherwise it simply contains the CA for validation
func CreateKeyAndTruststore(authenticate bool) (keystore []byte, truststore []byte, clientTLSConf *tls.Config) {
	ca := ca()
	server := cert("server", ca)
	keystore = createKeystore(ca, server)

	client := cert("client", ca)
	truststore = createTruststore(ca, client, authenticate)

	certpool := x509.NewCertPool()
	certpool.AddCert(ca.cert)

	clientTLSConf = &tls.Config{
		GetClientCertificate: func(t *tls.CertificateRequestInfo) (*tls.Certificate, error) {
			certificate, err := tls.X509KeyPair(client.getCertPEM(), client.getPrivateKeyPEM())
			return &certificate, err
		},
		RootCAs:    certpool,
		ServerName: ServerName,
	}
	return
}

func ca() *certHolder {
	// create our private and public key
	privateKey, err := rsa.GenerateKey(rand.Reader, keyBits)
	ExpectNoError(err)

	// create the CA
	ca := &x509.Certificate{
		SerialNumber: big.NewInt(2019),
		Subject: pkix.Name{
			CommonName:         "CA",
			Organization:       []string{"JBoss"},
			OrganizationalUnit: []string{"Infinispan"},
			Locality:           []string{"Red Hat"},
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().AddDate(10, 0, 0),
		IsCA:                  true,
		BasicConstraintsValid: true,
		PublicKeyAlgorithm:    x509.RSA,
		SignatureAlgorithm:    x509.SHA256WithRSA,
	}
	certBytes, err := x509.CreateCertificate(rand.Reader, ca, ca, &privateKey.PublicKey, privateKey)
	ExpectNoError(err)

	cert, err := x509.ParseCertificate(certBytes)
	ExpectNoError(err)

	return &certHolder{
		privateKey: privateKey,
		cert:       cert,
		certBytes:  certBytes,
	}
}

func cert(name string, ca *certHolder) *certHolder {
	// create our private and public key
	privateKey, err := rsa.GenerateKey(rand.Reader, keyBits)
	ExpectNoError(err)

	// set up our server certificate
	server := &x509.Certificate{
		SerialNumber: big.NewInt(serialNumber),
		Subject: pkix.Name{
			CommonName:         name,
			Organization:       []string{"JBoss"},
			OrganizationalUnit: []string{"Infinispan"},
			Locality:           []string{"Red Hat"},
		},
		Issuer:             ca.cert.Subject,
		NotBefore:          time.Now(),
		NotAfter:           time.Now().AddDate(10, 0, 0),
		PublicKeyAlgorithm: x509.RSA,
		SignatureAlgorithm: x509.SHA256WithRSA,
	}
	serialNumber++

	certBytes, err := x509.CreateCertificate(rand.Reader, server, ca.cert, &privateKey.PublicKey, ca.privateKey)
	ExpectNoError(err)

	cert, err := x509.ParseCertificate(certBytes)
	ExpectNoError(err)

	return &certHolder{
		privateKey: privateKey,
		cert:       cert,
		certBytes:  certBytes,
	}
}

func createKeystore(ca, server *certHolder) []byte {
	// It's not possible to use the p12 library as we get the following error
	// TLS handshake failed: javax.net.ssl.SSLException: error:1417A0C1:SSL routines:tls_post_process_client_hello:no shared cipher
	// keystore, err := p12.Encode(rand.Reader, server.privateKey, server.cert, []*x509.Certificate{ca.cert}, KeystorePassword)
	var fileMode os.FileMode = 0777
	ExpectNoError(os.MkdirAll(tmpDir, fileMode))
	defer os.RemoveAll(tmpDir)

	privKeyFile := tmpFile("server_key.pem")
	certFile := tmpFile("server_cert.pem")
	keystorefile := tmpFile("keystore.p12")

	err := ioutil.WriteFile(privKeyFile, server.getPrivateKeyPEM(), fileMode)
	ExpectNoError(err)

	err = ioutil.WriteFile(certFile, append(server.getCertPEM(), ca.getCertPEM()...), fileMode)
	ExpectNoError(err)

	cmd := exec.Command("openssl", "pkcs12", "-export", "-in", certFile, "-inkey", privKeyFile,
		"-name", server.cert.Subject.CommonName, "-out", keystorefile, "-password", "pass:"+KeystorePassword, "-noiter", "-nomaciter")
	ExpectNoError(cmd.Run())

	keystore, err := ioutil.ReadFile(keystorefile)
	ExpectNoError(err)
	return keystore
}

func createTruststore(ca, client *certHolder, authenticate bool) []byte {
	var trustCerts []*x509.Certificate
	// Only add the client certificate to the truststore if we require authentication
	if authenticate {
		trustCerts = []*x509.Certificate{ca.cert, client.cert}
	} else {
		trustCerts = []*x509.Certificate{ca.cert}
	}
	truststore, err := p12.EncodeTrustStore(rand.Reader, trustCerts, TruststorePassword)
	ExpectNoError(err)
	return truststore
}

func tmpFile(name string) string {
	return fmt.Sprintf("%s/%s", tmpDir, name)
}

// Return the private key in PEM format
func (c *certHolder) getPrivateKeyPEM() []byte {
	privKeyPEM := new(bytes.Buffer)
	err := pem.Encode(privKeyPEM, &pem.Block{
		Type:  keyutil.RSAPrivateKeyBlockType,
		Bytes: x509.MarshalPKCS1PrivateKey(c.privateKey),
	})
	ExpectNoError(err)
	return privKeyPEM.Bytes()
}

// Return the certificate in PEM format
func (c *certHolder) getCertPEM() []byte {
	cert := new(bytes.Buffer)
	err := pem.Encode(cert, &pem.Block{
		Type:  certUtil.CertificateBlockType,
		Bytes: c.certBytes,
	})
	ExpectNoError(err)
	return cert.Bytes()
}
