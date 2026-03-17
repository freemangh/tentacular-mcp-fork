package k8s

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"text/template"

	authv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// IssueToken creates a short-lived token for the tentacular-workflow ServiceAccount
// using the TokenRequest API. The token expires after ttlMinutes.
func IssueToken(ctx context.Context, client *Client, namespace string, ttlMinutes int) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()

	expirationSeconds := int64(ttlMinutes * 60)
	tokenRequest := &authv1.TokenRequest{
		Spec: authv1.TokenRequestSpec{
			ExpirationSeconds: &expirationSeconds,
		},
	}

	result, err := client.Clientset.CoreV1().ServiceAccounts(namespace).CreateToken(
		ctx,
		workflowServiceAccount,
		tokenRequest,
		metav1.CreateOptions{},
	)
	if err != nil {
		return "", fmt.Errorf("issue token for SA %q in namespace %q: %w", workflowServiceAccount, namespace, err)
	}
	return result.Status.Token, nil
}

var kubeconfigTemplate = template.Must(template.New("kubeconfig").Parse(`apiVersion: v1
kind: Config
clusters:
- cluster:
    certificate-authority-data: {{ .CAData }}
    server: {{ .Server }}
  name: tentacular
contexts:
- context:
    cluster: tentacular
    namespace: {{ .Namespace }}
    user: tentacular-workflow
  name: tentacular
current-context: tentacular
users:
- name: tentacular-workflow
  user:
    token: {{ .Token }}
`))

type kubeconfigData struct {
	Server    string
	CAData    string
	Token     string
	Namespace string
}

// GenerateKubeconfig produces a complete kubeconfig YAML string with the given
// cluster URL, CA certificate, token, and namespace.
func GenerateKubeconfig(clusterURL, caCert, token, namespace string) (string, error) {
	data := kubeconfigData{
		Server:    clusterURL,
		CAData:    base64.StdEncoding.EncodeToString([]byte(caCert)),
		Token:     token,
		Namespace: namespace,
	}

	var buf bytes.Buffer
	if err := kubeconfigTemplate.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("render kubeconfig template: %w", err)
	}
	return buf.String(), nil
}
