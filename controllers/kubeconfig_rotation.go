/*
Copyright 2025 Huawei Cloud.

Licensed under the MIT No Attribution (MIT-0) License.
*/

package controllers

import (
	"context"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// kubeconfigRefreshThresholdDays: refresh the kubeconfig Secret when the client
// certificate expires within this many days (questionnaire Q2 — validity is
// -1/[1,1827] days; we request 365 and rotate before 30 days remain).
const kubeconfigRefreshThresholdDays = 30

// kubeconfigNeedsRefresh reports whether the stored kubeconfig is missing or
// its client certificate expires within minDays. On any parse error it returns
// true so the controller re-fetches the certificate.
func kubeconfigNeedsRefresh(ctx context.Context, c client.Client, namespace, secretName string, minDays int) bool {
	if secretName == "" {
		return true
	}
	secret := &corev1.Secret{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: namespace, Name: secretName}, secret); err != nil {
		return true
	}
	value, ok := secret.Data["value"]
	if !ok || len(value) == 0 {
		return true
	}
	expiry, ok := kubeconfigClientCertExpiry(value)
	if !ok {
		return true
	}
	return time.Until(expiry) < time.Duration(minDays)*24*time.Hour
}

// kubeconfigClientCertExpiry parses a kubeconfig and returns the notAfter time
// of the first usable client certificate.
func kubeconfigClientCertExpiry(data []byte) (time.Time, bool) {
	cfg, err := clientcmd.Load(data)
	if err != nil {
		return time.Time{}, false
	}
	for _, auth := range cfg.AuthInfos {
		if auth == nil || len(auth.ClientCertificateData) == 0 {
			continue
		}
		der := make([]byte, len(auth.ClientCertificateData))
		copy(der, auth.ClientCertificateData)
		// The data may be base64 of a PEM or raw DER certificate.
		if decoded, err := base64.StdEncoding.DecodeString(string(der)); err == nil {
			der = decoded
		}
		if block, _ := pem.Decode(der); block != nil {
			der = block.Bytes
		}
		cert, err := x509.ParseCertificate(der)
		if err != nil {
			continue
		}
		return cert.NotAfter, true
	}
	return time.Time{}, false
}
