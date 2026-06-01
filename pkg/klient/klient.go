// Copyright 2026 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package klient provides a simplified, lightweight Kubernetes port-forwarding
// client without pulling in any k8s.io/client-go dependencies.
package klient

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/moby/spdystream"
	"gopkg.in/yaml.v3"
)

// Client is a lightweight Kubernetes client capable of port-forwarding to pods
// without any dependency on k8s.io/client-go.
type Client struct {
	Host      string
	TLSConfig *tls.Config
	Token     string
}

// KubeConfig represents the standard KubeConfig YAML structure.
type KubeConfig struct {
	APIVersion     string `yaml:"apiVersion"`
	Kind           string `yaml:"kind"`
	CurrentContext string `yaml:"current-context"`
	Clusters       []struct {
		Name    string `yaml:"name"`
		Cluster struct {
			Server                   string `yaml:"server"`
			CertificateAuthority     string `yaml:"certificate-authority"`
			CertificateAuthorityData string `yaml:"certificate-authority-data"`
			InsecureSkipTLSVerify    bool   `yaml:"insecure-skip-tls-verify"`
		} `yaml:"cluster"`
	} `yaml:"clusters"`
	Users []struct {
		Name string `yaml:"name"`
		User struct {
			Token                  string `yaml:"token"`
			ClientCertificate      string `yaml:"client-certificate"`
			ClientCertificateData  string `yaml:"client-certificate-data"`
			ClientKey              string `yaml:"client-key"`
			ClientKeyData          string `yaml:"client-key-data"`
		} `yaml:"user"`
	} `yaml:"users"`
	Contexts []struct {
		Name    string `yaml:"name"`
		Context struct {
			Cluster string `yaml:"cluster"`
			User    string `yaml:"user"`
		} `yaml:"context"`
	} `yaml:"contexts"`
}

// NewClient automatically detects the environment and returns a Client.
// It will first check if it is running in-cluster, and fallback to
// out-of-cluster configuration (loading KubeConfig).
func NewClient() (*Client, error) {
	tokenFile := "/var/run/secrets/kubernetes.io/serviceaccount/token"
	caFile := "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"
	hostEnv := os.Getenv("KUBERNETES_SERVICE_HOST")
	portEnv := os.Getenv("KUBERNETES_SERVICE_PORT")

	if hostEnv != "" && portEnv != "" {
		token, err := os.ReadFile(tokenFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read in-cluster token: %w", err)
		}

		caBytes, err := os.ReadFile(caFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read in-cluster CA certificate: %w", err)
		}

		caPool := x509.NewCertPool()
		if !caPool.AppendCertsFromPEM(caBytes) {
			return nil, fmt.Errorf("failed to parse in-cluster CA certificate")
		}

		tlsConfig := &tls.Config{
			RootCAs: caPool,
		}

		return &Client{
			Host:      fmt.Sprintf("https://%s:%s", hostEnv, portEnv),
			TLSConfig: tlsConfig,
			Token:     strings.TrimSpace(string(token)),
		}, nil
	}

	return LoadConfig("")
}

// LoadConfig parses a kubeconfig file at the given path.
// If configPath is empty, it uses the KUBECONFIG environment variable or
// defaults to ~/.kube/config.
func LoadConfig(configPath string) (*Client, error) {
	if configPath == "" {
		configPath = os.Getenv("KUBECONFIG")
		if configPath == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return nil, fmt.Errorf("could not get user home directory: %w", err)
			}
			configPath = filepath.Join(home, ".kube", "config")
		}
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("could not read kubeconfig at %s: %w", configPath, err)
	}

	var cfg KubeConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("could not parse kubeconfig: %w", err)
	}

	contextName := cfg.CurrentContext
	if contextName == "" {
		if len(cfg.Contexts) == 0 {
			return nil, fmt.Errorf("no contexts found in kubeconfig")
		}
		contextName = cfg.Contexts[0].Name
	}

	var activeContext struct {
		Cluster string
		User    string
	}
	foundContext := false
	for _, ctx := range cfg.Contexts {
		if ctx.Name == contextName {
			activeContext.Cluster = ctx.Context.Cluster
			activeContext.User = ctx.Context.User
			foundContext = true
			break
		}
	}
	if !foundContext {
		return nil, fmt.Errorf("context %q not found in kubeconfig", contextName)
	}

	var activeCluster struct {
		Server                   string
		CertificateAuthority     string
		CertificateAuthorityData string
		InsecureSkipTLSVerify    bool
	}
	foundCluster := false
	for _, c := range cfg.Clusters {
		if c.Name == activeContext.Cluster {
			activeCluster.Server = c.Cluster.Server
			activeCluster.CertificateAuthority = c.Cluster.CertificateAuthority
			activeCluster.CertificateAuthorityData = c.Cluster.CertificateAuthorityData
			activeCluster.InsecureSkipTLSVerify = c.Cluster.InsecureSkipTLSVerify
			foundCluster = true
			break
		}
	}
	if !foundCluster {
		return nil, fmt.Errorf("cluster %q not found in kubeconfig", activeContext.Cluster)
	}

	var activeUser struct {
		Token                 string
		ClientCertificate     string
		ClientCertificateData string
		ClientKey             string
		ClientKeyData         string
	}
	for _, u := range cfg.Users {
		if u.Name == activeContext.User {
			activeUser.Token = u.User.Token
			activeUser.ClientCertificate = u.User.ClientCertificate
			activeUser.ClientCertificateData = u.User.ClientCertificateData
			activeUser.ClientKey = u.User.ClientKey
			activeUser.ClientKeyData = u.User.ClientKeyData
			break
		}
	}

	tlsConfig := &tls.Config{
		InsecureSkipVerify: activeCluster.InsecureSkipTLSVerify,
	}

	if !activeCluster.InsecureSkipTLSVerify {
		var caBytes []byte
		if activeCluster.CertificateAuthorityData != "" {
			var err error
			caBytes, err = base64.StdEncoding.DecodeString(activeCluster.CertificateAuthorityData)
			if err != nil {
				return nil, fmt.Errorf("could not decode certificate-authority-data: %w", err)
			}
		} else if activeCluster.CertificateAuthority != "" {
			caPath := activeCluster.CertificateAuthority
			if !filepath.IsAbs(caPath) {
				caPath = filepath.Join(filepath.Dir(configPath), caPath)
			}
			var err error
			caBytes, err = os.ReadFile(caPath)
			if err != nil {
				return nil, fmt.Errorf("could not read certificate-authority file: %w", err)
			}
		}

		if len(caBytes) > 0 {
			caPool := x509.NewCertPool()
			if !caPool.AppendCertsFromPEM(caBytes) {
				return nil, fmt.Errorf("could not append CA certs to pool")
			}
			tlsConfig.RootCAs = caPool
		}
	}

	var certBytes, keyBytes []byte
	if activeUser.ClientCertificateData != "" {
		var err error
		certBytes, err = base64.StdEncoding.DecodeString(activeUser.ClientCertificateData)
		if err != nil {
			return nil, fmt.Errorf("could not decode client-certificate-data: %w", err)
		}
	} else if activeUser.ClientCertificate != "" {
		certPath := activeUser.ClientCertificate
		if !filepath.IsAbs(certPath) {
			certPath = filepath.Join(filepath.Dir(configPath), certPath)
		}
		var err error
		certBytes, err = os.ReadFile(certPath)
		if err != nil {
			return nil, fmt.Errorf("could not read client-certificate file: %w", err)
		}
	}

	if activeUser.ClientKeyData != "" {
		var err error
		keyBytes, err = base64.StdEncoding.DecodeString(activeUser.ClientKeyData)
		if err != nil {
			return nil, fmt.Errorf("could not decode client-key-data: %w", err)
		}
	} else if activeUser.ClientKey != "" {
		keyPath := activeUser.ClientKey
		if !filepath.IsAbs(keyPath) {
			keyPath = filepath.Join(filepath.Dir(configPath), keyPath)
		}
		var err error
		keyBytes, err = os.ReadFile(keyPath)
		if err != nil {
			return nil, fmt.Errorf("could not read client-key file: %w", err)
		}
	}

	if len(certBytes) > 0 && len(keyBytes) > 0 {
		cert, err := tls.X509KeyPair(certBytes, keyBytes)
		if err != nil {
			return nil, fmt.Errorf("could not load x509 client key pair: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	}

	return &Client{
		Host:      activeCluster.Server,
		TLSConfig: tlsConfig,
		Token:     activeUser.Token,
	}, nil
}

// Dial establishes an in-process port forwarding stream (wrapped in a net.Conn)
// to the specified pod name and port inside the given namespace.
func (c *Client) Dial(ctx context.Context, namespace, podName string, port int) (net.Conn, error) {
	u, err := url.Parse(c.Host)
	if err != nil {
		return nil, fmt.Errorf("invalid API host URL: %w", err)
	}

	targetAddr := u.Host
	if !strings.Contains(targetAddr, ":") {
		if u.Scheme == "https" {
			targetAddr = targetAddr + ":443"
		} else {
			targetAddr = targetAddr + ":80"
		}
	}

	var d net.Dialer
	rawConn, err := d.DialContext(ctx, "tcp", targetAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to dial API server: %w", err)
	}

	// Watch for context cancellation to interrupt blocking connection handshakes.
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			_ = rawConn.Close()
		case <-done:
		}
	}()

	tlsConfig := c.TLSConfig
	if tlsConfig == nil {
		tlsConfig = &tls.Config{}
	} else {
		tlsConfig = tlsConfig.Clone()
	}

	hostName, _, err := net.SplitHostPort(targetAddr)
	if err != nil {
		hostName = targetAddr
	}
	tlsConfig.ServerName = hostName

	var conn net.Conn = rawConn
	if u.Scheme == "https" {
		tlsConn := tls.Client(rawConn, tlsConfig)
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			return nil, fmt.Errorf("tls handshake with API server failed: %w", err)
		}
		conn = tlsConn
	}

	path := fmt.Sprintf("/api/v1/namespaces/%s/pods/%s/portforward", namespace, podName)
	reqURL := &url.URL{
		Scheme:   u.Scheme,
		Host:     u.Host,
		Path:     path,
		RawQuery: fmt.Sprintf("ports=%d", port),
	}

	req, err := http.NewRequestWithContext(ctx, "POST", reqURL.String(), nil)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("failed to create upgrade request: %w", err)
	}

	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	req.Header.Set("Upgrade", "SPDY/3.1")
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("X-Stream-Protocol-Version", "portforward.k8s.io")

	if err := req.Write(conn); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("failed to send upgrade request: %w", err)
	}

	bufReader := bufio.NewReader(conn)
	resp, err := http.ReadResponse(bufReader, req)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("failed to read upgrade response: %w", err)
	}

	if resp.StatusCode != http.StatusSwitchingProtocols {
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		_ = conn.Close()
		return nil, fmt.Errorf("handshake failed with status %d: %s", resp.StatusCode, string(body))
	}
	_ = resp.Body.Close()

	bc := &bufferedConn{
		Conn:   conn,
		reader: io.MultiReader(io.LimitReader(bufReader, int64(bufReader.Buffered())), conn),
	}

	spdyConn, err := spdystream.NewConnection(bc, false)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("failed to establish spdy session: %w", err)
	}
	go spdyConn.Serve(spdystream.NoOpStreamHandler)

	headers := http.Header{}
	headers.Set("StreamType", "data")
	headers.Set("Port", fmt.Sprintf("%d", port))

	dataStream, err := spdyConn.CreateStream(headers, nil, false)
	if err != nil {
		_ = spdyConn.Close()
		_ = conn.Close()
		return nil, fmt.Errorf("failed to create spdy data stream: %w", err)
	}

	errHeaders := http.Header{}
	errHeaders.Set("StreamType", "error")
	errHeaders.Set("Port", fmt.Sprintf("%d", port))

	_, err = spdyConn.CreateStream(errHeaders, nil, false)
	if err != nil {
		_ = dataStream.Close()
		_ = spdyConn.Close()
		_ = conn.Close()
		return nil, fmt.Errorf("failed to create spdy error stream: %w", err)
	}

	if err := dataStream.Wait(); err != nil {
		_ = dataStream.Close()
		_ = spdyConn.Close()
		_ = conn.Close()
		return nil, fmt.Errorf("failed to wait for spdy data stream: %w", err)
	}

	return &portForwardConn{
		Stream:      dataStream,
		spdySession: spdyConn,
		underlying:  conn,
	}, nil
}

type bufferedConn struct {
	net.Conn
	reader io.Reader
}

func (bc *bufferedConn) Read(b []byte) (int, error) {
	return bc.reader.Read(b)
}

type portForwardConn struct {
	*spdystream.Stream
	spdySession *spdystream.Connection
	underlying  net.Conn
}

func (p *portForwardConn) Close() error {
	var errs []string
	if err := p.Stream.Close(); err != nil {
		errs = append(errs, err.Error())
	}
	if err := p.spdySession.Close(); err != nil {
		errs = append(errs, err.Error())
	}
	if err := p.underlying.Close(); err != nil {
		errs = append(errs, err.Error())
	}
	if len(errs) > 0 {
		return fmt.Errorf("errors closing connection: %s", strings.Join(errs, ", "))
	}
	return nil
}

func (p *portForwardConn) LocalAddr() net.Addr {
	return p.underlying.LocalAddr()
}

func (p *portForwardConn) RemoteAddr() net.Addr {
	return p.underlying.RemoteAddr()
}

func (p *portForwardConn) SetDeadline(t time.Time) error {
	return p.underlying.SetDeadline(t)
}

func (p *portForwardConn) SetReadDeadline(t time.Time) error {
	return p.underlying.SetReadDeadline(t)
}

func (p *portForwardConn) SetWriteDeadline(t time.Time) error {
	return p.underlying.SetWriteDeadline(t)
}
