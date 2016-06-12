package testutil

import (
	"bufio"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"io/ioutil"
	. "testing"

	"github.com/stretchr/testify/require"
)

func TestSignedCert(t *T) {
	domain := RandStr() + ".ninja"

	caCert, caKey := SignedCert("", nil, nil)
	caCertDER, _ := pem.Decode(caCert)
	require.NotEmpty(t, caCertDER)
	caCertx509, err := x509.ParseCertificate(caCertDER.Bytes)
	require.Nil(t, err)

	caPool := x509.NewCertPool()
	caPool.AddCert(caCertx509)

	cert, key := SignedCert(domain, caCert, caKey)

	tlscert, err := tls.X509KeyPair(cert, key)
	require.Nil(t, err)

	srvTLSConf := &tls.Config{
		Certificates: []tls.Certificate{tlscert},
	}
	srvTLSConf.BuildNameToCertificate()
	l, err := tls.Listen("tcp", ":0", srvTLSConf)
	require.Nil(t, err)
	addr := l.Addr().String()

	go func() {
		c, err := l.Accept()
		require.Nil(t, err)

		b, err := bufio.NewReader(c).ReadString('\n')
		require.Nil(t, err)
		c.Write([]byte(b))
		c.Close()
	}()

	clientTLSConf := &tls.Config{
		RootCAs:    caPool,
		ServerName: domain,
	}
	c, err := tls.Dial("tcp", addr, clientTLSConf)
	require.Nil(t, err)

	s := RandStr() + "\n"
	c.Write([]byte(s))
	out, err := ioutil.ReadAll(c)
	require.Nil(t, err)
	require.Equal(t, s, string(out))
}
