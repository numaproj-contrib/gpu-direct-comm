/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package dns

import (
	"fmt"
	"net"
	"net/url"
	"testing"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
	"go.etcd.io/etcd/server/v3/embed"
)

// newEmbeddedEtcdClient starts an embedded etcd server for testing and returns
// a connected client. The server is stopped when the test finishes.
func newEmbeddedEtcdClient(t *testing.T) *clientv3.Client {
	t.Helper()

	dir := t.TempDir()

	clientPort := freePort(t)
	peerPort := freePort(t)

	clientURL := fmt.Sprintf("http://127.0.0.1:%d", clientPort)
	peerURL := fmt.Sprintf("http://127.0.0.1:%d", peerPort)

	cfg := embed.NewConfig()
	cfg.Dir = dir
	cfg.LogLevel = "error"

	lcURL, _ := url.Parse(clientURL)
	lpURL, _ := url.Parse(peerURL)
	cfg.ListenClientUrls = []url.URL{*lcURL}
	cfg.AdvertiseClientUrls = []url.URL{*lcURL}
	cfg.ListenPeerUrls = []url.URL{*lpURL}
	cfg.AdvertisePeerUrls = []url.URL{*lpURL}
	cfg.InitialCluster = fmt.Sprintf("default=%s", peerURL)

	e, err := embed.StartEtcd(cfg)
	if err != nil {
		t.Fatalf("start embedded etcd: %v", err)
	}

	select {
	case <-e.Server.ReadyNotify():
	case <-time.After(10 * time.Second):
		e.Close()
		t.Fatal("embedded etcd did not become ready in time")
	}

	t.Cleanup(func() {
		e.Close()
	})

	client, err := clientv3.New(clientv3.Config{
		Endpoints:   []string{clientURL},
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("create etcd client: %v", err)
	}
	t.Cleanup(func() {
		_ = client.Close()
	})

	return client
}

// freePort returns an available TCP port on localhost.
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("find free port: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	return port
}
