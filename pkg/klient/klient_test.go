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

package klient

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/moby/spdystream"
)

func TestLoadConfig(t *testing.T) {
	tempFile, err := os.CreateTemp("", "kubeconfig")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tempFile.Name())

	mockKubeconfig := `
apiVersion: v1
kind: Config
current-context: dev-context
clusters:
- name: dev-cluster
  cluster:
    server: https://127.0.0.1:8443
    insecure-skip-tls-verify: true
users:
- name: dev-user
  user:
    token: secret-token
contexts:
- name: dev-context
  context:
    cluster: dev-cluster
    user: dev-user
`
	if _, err := tempFile.Write([]byte(mockKubeconfig)); err != nil {
		t.Fatalf("failed to write mock kubeconfig: %v", err)
	}
	tempFile.Close()

	client, err := LoadConfig(tempFile.Name())
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if client.Host != "https://127.0.0.1:8443" {
		t.Errorf("expected host https://127.0.0.1:8443, got %s", client.Host)
	}
	if client.Token != "secret-token" {
		t.Errorf("expected token secret-token, got %s", client.Token)
	}
	if client.TLSConfig == nil || !client.TLSConfig.InsecureSkipVerify {
		t.Errorf("expected insecure skip verify to be true")
	}
}

func TestPortForward(t *testing.T) {
	t.Log("Starting TestPortForward")
	// Start a mock TLS server that handles the SPDY port-forward upgrade
	ts := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Log("Server: received request")
		if r.Header.Get("Upgrade") != "SPDY/3.1" {
			t.Log("Server: Upgrade header not SPDY/3.1")
			http.Error(w, "expected SPDY/3.1 upgrade", http.StatusBadRequest)
			return
		}

		// Hijack connection
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Log("Server: Hijacker not supported")
			http.Error(w, "webserver doesn't support hijacking", http.StatusInternalServerError)
			return
		}
		conn, _, err := hj.Hijack()
		if err != nil {
			t.Logf("Server: Hijack failed: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer conn.Close()
		t.Log("Server: Connection hijacked successfully")

		// Write switching protocols response
		resp := "HTTP/1.1 101 Switching Protocols\r\n" +
			"Upgrade: SPDY/3.1\r\n" +
			"Connection: Upgrade\r\n" +
			"X-Stream-Protocol-Version: portforward.k8s.io\r\n\r\n"
		_, err = conn.Write([]byte(resp))
		if err != nil {
			t.Logf("Server: Failed to write HTTP 101: %v", err)
			return
		}
		t.Log("Server: HTTP 101 response written")

		// Initialize SPDY connection on hijacked conn (server-side, so isServer = true)
		spdyConn, err := spdystream.NewConnection(conn, true)
		if err != nil {
			t.Logf("Server: Failed to create spdy connection: %v", err)
			return
		}
		t.Log("Server: SPDY connection created")

		// Wait for streams
		streamChan := make(chan *spdystream.Stream, 2)
		go func() {
			t.Log("Server: Starting spdyConn.Serve")
			spdyConn.Serve(func(stream *spdystream.Stream) {
				t.Logf("Server: Received new SPDY stream type=%s", stream.Headers().Get("StreamType"))
				err := stream.SendReply(http.Header{}, false)
				if err != nil {
					t.Logf("Server: Failed to send reply: %v", err)
				} else {
					t.Log("Server: Sent reply to stream")
				}
				streamChan <- stream
			})
			t.Log("Server: spdyConn.Serve exited")
		}()

		var dataStream, errStream *spdystream.Stream
		for i := 0; i < 2; i++ {
			select {
			case s := <-streamChan:
				if s.Headers().Get("StreamType") == "data" {
					dataStream = s
				} else if s.Headers().Get("StreamType") == "error" {
					errStream = s
				}
			case <-time.After(2 * time.Second):
				t.Log("Server: Timeout waiting for streams on streamChan")
				return
			}
		}

		if dataStream == nil || errStream == nil {
			t.Logf("Server: Missing stream: data=%v, err=%v", dataStream != nil, errStream != nil)
			return
		}

		// Read data from data stream
		t.Log("Server: Reading from dataStream")
		buf := make([]byte, 100)
		n, err := dataStream.Read(buf)
		if err != nil {
			t.Logf("Server: Failed to read from dataStream: %v", err)
			return
		}

		received := string(buf[:n])
		t.Logf("Server: Received data: %q", received)
		if received != "hello" {
			t.Errorf("unexpected data received: %q", received)
			return
		}

		// Write response
		t.Log("Server: Writing to dataStream")
		_, err = dataStream.Write([]byte("world"))
		if err != nil {
			t.Logf("Server: Failed to write to dataStream: %v", err)
			return
		}
		t.Log("Server: Response written")

		// Wait a brief moment before exiting to ensure clean flush
		time.Sleep(100 * time.Millisecond)
	}))

	ts.StartTLS()
	defer ts.Close()

	t.Logf("Server running on %s", ts.URL)

	// Configure client to talk to our mock server
	client := &Client{
		Host: ts.URL,
		TLSConfig: &tls.Config{
			InsecureSkipVerify: true,
		},
		Token: "test-token",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	t.Log("Client: Dialing")
	conn, err := client.Dial(ctx, "test-namespace", "test-pod", 8080)
	if err != nil {
		t.Fatalf("Dial failed: %v", err)
	}
	defer conn.Close()
	t.Log("Client: Dialed successfully")

	// Write data to port forward connection
	t.Log("Client: Writing data")
	_, err = conn.Write([]byte("hello"))
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	t.Log("Client: Data written successfully")

	// Read response
	t.Log("Client: Reading response")
	buf := make([]byte, 100)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}

	response := string(buf[:n])
	t.Logf("Client: Received response: %q", response)
	if response != "world" {
		t.Errorf("expected response 'world', got %q", response)
	}
}
