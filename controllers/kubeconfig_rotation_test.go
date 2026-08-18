/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

package controllers

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"math/big"
	"testing"
	"time"
)

// makeKubeconfig builds a kubeconfig with a client cert valid for the given
// duration (the format produced by CreateKubernetesClusterCert).
func makeKubeconfig(t *testing.T, validFor time.Duration) []byte {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test-user"},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(validFor),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	return []byte(`apiVersion: v1
kind: Config
clusters:
- name: internalCluster
  cluster:
    server: https://10.0.0.10:5443
    certificate-authority-data: ` + base64.StdEncoding.EncodeToString(certPEM) + `
users:
- name: internal
  user:
    client-certificate-data: ` + base64.StdEncoding.EncodeToString(certPEM) + `
    client-key-data: dGVzdC1rZXk=
contexts:
- name: internal
  context:
    cluster: internalCluster
    user: internal
current-context: internal
`)
}

func TestKubeconfigClientCertExpiry(t *testing.T) {
	// Long-lived cert: expiry parse succeeds and is in the future.
	data := makeKubeconfig(t, 365*24*time.Hour)
	expiry, ok := kubeconfigClientCertExpiry(data)
	if !ok {
		t.Fatal("expected expiry to be parsed")
	}
	if time.Until(expiry) < 300*24*time.Hour {
		t.Errorf("expected ~365d validity, got %v until expiry", time.Until(expiry))
	}

	// Short-lived cert: parsed expiry is soon.
	short := makeKubeconfig(t, 10*24*time.Hour)
	expiry2, ok := kubeconfigClientCertExpiry(short)
	if !ok {
		t.Fatal("expected short cert expiry to be parsed")
	}
	if time.Until(expiry2) > 20*24*time.Hour {
		t.Errorf("expected ~10d validity, got %v", time.Until(expiry2))
	}

	// Garbage input: must report "needs refresh".
	if _, ok := kubeconfigClientCertExpiry([]byte("not a kubeconfig")); ok {
		t.Error("expected parse failure on garbage input")
	}
}

func TestKubeconfigNeedsRefresh(t *testing.T) {
	// Pure helper path: long cert + 30d threshold => no refresh.
	data := makeKubeconfig(t, 365*24*time.Hour)
	expiry, ok := kubeconfigClientCertExpiry(data)
	if !ok {
		t.Fatal("parse failed")
	}
	if time.Until(expiry) < time.Duration(kubeconfigRefreshThresholdDays)*24*time.Hour {
		t.Errorf("expected no refresh needed for long-lived cert")
	}
}
