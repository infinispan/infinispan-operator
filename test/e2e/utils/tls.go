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
	"math"
	"math/big"
	"os"
	"os/exec"
	"time"

	"github.com/infinispan/infinispan-operator/controllers/constants"
	certUtil "k8s.io/client-go/util/cert"
	"k8s.io/client-go/util/keyutil"
)

const (
	KeystorePassword   = "SuperSecret22"
	TruststorePassword = "SuperSecret22"
	keyBits            = 2048
)

type certHolder struct {
	privateKey *rsa.PrivateKey
	cert       *x509.Certificate
	certBytes  []byte
}

// CreateServerCertificates returns the public and private keys o
func CreateServerCertificates(serverName string) (publicKey, privateKey []byte, clientTLSConf *tls.Config) {
	ca := ca()
	server := serverCert(serverName, ca)
	publicKey = server.getCertPEM()
	privateKey = server.getPrivateKeyPEM()

	certpool := x509.NewCertPool()
	certpool.AddCert(ca.cert)
	clientTLSConf = &tls.Config{
		RootCAs:    certpool,
		ServerName: serverName,
	}
	return
}

// CreateKeystore returns a keystore using a self-signed certificate, and the corresponding tls.Config required by clients to connect to the server
func CreateKeystore(serverName string) (keystore []byte, clientTLSConf *tls.Config) {
	ca := ca()
	server := serverCert(serverName, ca)
	keystore = createKeystore(ca, server)

	certpool := x509.NewCertPool()
	certpool.AddCert(ca.cert)
	clientTLSConf = &tls.Config{
		RootCAs:    certpool,
		ServerName: serverName,
	}
	return
}

func CreateKeystoreAndClientCerts(serverName string) (keystore []byte, caPem []byte, clientPem []byte, clientTLSConf *tls.Config) {
	ca := ca()
	server := serverCert(serverName, ca)
	keystore = createKeystore(ca, server)
	client := clientCert("client", ca)

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
		ServerName: serverName,
	}
	return
}

// CreateKeyAndTruststore returns a keystore & truststore using a self-signed certificate, and the corresponding tls.Config required by clients to connect to the server
// If authenticate is true, then the returned truststore contains all client certificates, otherwise it simply contains the CA for validation
func CreateKeyAndTruststore(serverName string, authenticate bool) (keystore []byte, truststore []byte, clientTLSConf *tls.Config) {
	ca := ca()
	server := serverCert(serverName, ca)
	keystore = createKeystore(ca, server)

	client := clientCert("client", ca)
	truststore = createTruststore(ca, client, authenticate)

	certpool := x509.NewCertPool()
	certpool.AddCert(ca.cert)

	clientTLSConf = &tls.Config{
		GetClientCertificate: func(t *tls.CertificateRequestInfo) (*tls.Certificate, error) {
			certificate, err := tls.X509KeyPair(client.getCertPEM(), client.getPrivateKeyPEM())
			return &certificate, err
		},
		RootCAs:    certpool,
		ServerName: serverName,
	}
	return
}

type KeyCertPair struct {
	PrivateKey  []byte
	Certificate []byte
}

func CreateKeyCertAndTruststore(serverName string, authenticate bool) (keyCertPair KeyCertPair, truststore []byte, clientTLSConf *tls.Config) {
	ca := ca()
	server := serverCert(serverName, ca)

	keyCertPair = KeyCertPair{
		Certificate: server.getCertPEM(),
		PrivateKey:  server.getPrivateKeyPEM(),
	}

	client := clientCert("client", ca)
	truststore = createTruststore(ca, client, authenticate)

	certpool := x509.NewCertPool()
	certpool.AddCert(ca.cert)

	clientTLSConf = &tls.Config{
		GetClientCertificate: func(t *tls.CertificateRequestInfo) (*tls.Certificate, error) {
			certificate, err := tls.X509KeyPair(client.getCertPEM(), client.getPrivateKeyPEM())
			return &certificate, err
		},
		RootCAs:    certpool,
		ServerName: serverName,
	}
	return
}

func CreateDefaultCrossSiteKeyAndTrustStore() (transportKeyStore, routerKeyStore, trustStore []byte) {
	ca := ca()
	transportCert := createGenericCertificate(constants.DefaultSiteTransportKeyStoreAlias, nil, ca)
	routerCert := createGenericCertificate(constants.DefaultSiteRouterKeyStoreAlias, nil, ca)

	transportKeyStore = createKeystore(ca, transportCert)
	routerKeyStore = createKeystore(ca, routerCert)

	trustStore = createGenericTruststore(ca)
	return
}

func CreateCrossSiteSingleKeyStoreAndTrustStore() (KeyStore, trustStore []byte) {
	ca := ca()

	transportCert := createGenericCertificate("same", nil, ca)
	KeyStore = createKeystore(ca, transportCert)
	trustStore = createGenericTruststore(ca)
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

func serverCert(dnsName string, ca *certHolder) *certHolder {
	return createGenericCertificate("server", &dnsName, ca)
}

func clientCert(name string, ca *certHolder) *certHolder {
	return createGenericCertificate(name, nil, ca)
}

func createGenericCertificate(name string, dnsName *string, ca *certHolder) *certHolder {
	// create our private and public key
	privateKey, err := rsa.GenerateKey(rand.Reader, keyBits)
	ExpectNoError(err)

	serialNumber, err := rand.Int(rand.Reader, big.NewInt(math.MaxInt64))
	if err != nil {
		panic(err)
	}

	// set up our certificate
	cert := &x509.Certificate{
		SerialNumber: serialNumber,
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
	if dnsName != nil {
		cert.DNSNames = []string{*dnsName}
	}
	return createAndParseCert(cert, privateKey, ca)
}

func createAndParseCert(c *x509.Certificate, privateKey *rsa.PrivateKey, ca *certHolder) *certHolder {
	certBytes, err := x509.CreateCertificate(rand.Reader, c, ca.cert, &privateKey.PublicKey, ca.privateKey)
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
	tmpDir, err := os.MkdirTemp("", "operator-tls")
	ExpectNoError(err)
	defer os.RemoveAll(tmpDir)

	log.Info("Creating keystore.", "Temp Directory", tmpDir)

	privKeyFile := tmpFile(tmpDir, "server_key.pem")
	certFile := tmpFile(tmpDir, "server_cert.pem")
	keystorefile := tmpFile(tmpDir, "keystore.p12")

	err = os.WriteFile(privKeyFile, server.getPrivateKeyPEM(), fileMode)
	ExpectNoError(err)

	err = os.WriteFile(certFile, append(server.getCertPEM(), ca.getCertPEM()...), fileMode)
	ExpectNoError(err)

	cmd := exec.Command("openssl", "pkcs12", "-export", "-in", certFile, "-inkey", privKeyFile,
		"-name", server.cert.Subject.CommonName, "-out", keystorefile, "-password", "pass:"+KeystorePassword, "-noiter", "-nomaciter")
	ExpectNoError(cmd.Run())

	keystore, err := os.ReadFile(keystorefile)
	ExpectNoError(err)
	return keystore
}

func createTruststore(ca, client *certHolder, authenticate bool) []byte {
	// Only add the client certificate to the truststore if we require authentication
	if authenticate {
		return createGenericTruststore(ca, client)
	} else {
		return createGenericTruststore(ca)
	}
}

func createGenericTruststore(certs ...*certHolder) []byte {
	var fileMode os.FileMode = 0777
	tmpDir, err := os.MkdirTemp("", "operator-tls")
	ExpectNoError(err)
	defer os.RemoveAll(tmpDir)

	log.Info("Creating truststore.", "Temp Directory", tmpDir)

	trustStoreFile := tmpFile(tmpDir, "truststore.p12")
	for _, cert := range certs {
		certFile := tmpFile(tmpDir, "cert.pem")

		err := os.WriteFile(certFile, cert.getCertPEM(), fileMode)
		ExpectNoError(err)
		cmd := exec.Command("keytool", "-import", "-file", certFile, "-alias", cert.cert.Subject.CommonName,
			"-keystore", trustStoreFile, "-storepass", TruststorePassword, "-noprompt")
		ExpectNoError(cmd.Run())
	}

	truststore, err := os.ReadFile(trustStoreFile)
	ExpectNoError(err)
	return truststore
}

func tmpFile(tmpDir, name string) string {
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
