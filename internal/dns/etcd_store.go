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
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
)

const (
	defaultDialTimeout    = 5 * time.Second
	defaultRequestTimeout = 5 * time.Second
	skyDNSPrefix          = "/skydns"
)

// skyDNSRecord is the JSON format that CoreDNS etcd plugin expects.
type skyDNSRecord struct {
	Host string `json:"host"`
}

// EtcdStore implements Store using etcd v3 as the backend.
type EtcdStore struct {
	client *clientv3.Client
}

// Option configures EtcdStore construction.
type Option func(*etcdStoreConfig)

type etcdStoreConfig struct {
	dialTimeout time.Duration
}

// WithDialTimeout overrides the default etcd dial timeout.
func WithDialTimeout(d time.Duration) Option {
	return func(c *etcdStoreConfig) {
		c.dialTimeout = d
	}
}

// NewEtcdStore creates a new EtcdStore connected to the given endpoints.
func NewEtcdStore(endpoints []string, opts ...Option) (*EtcdStore, error) {
	cfg := &etcdStoreConfig{dialTimeout: defaultDialTimeout}
	for _, o := range opts {
		o(cfg)
	}

	client, err := clientv3.New(clientv3.Config{
		Endpoints:   endpoints,
		DialTimeout: cfg.dialTimeout,
	})
	if err != nil {
		return nil, fmt.Errorf("create etcd client: %w", err)
	}
	return &EtcdStore{client: client}, nil
}

// NewEtcdStoreFromClient creates an EtcdStore from an existing etcd client.
// This is useful for testing with embedded etcd.
func NewEtcdStoreFromClient(client *clientv3.Client) *EtcdStore {
	return &EtcdStore{client: client}
}

// Put creates or updates an A record for (fqdn, podID) → ip.
// The etcd key is structured as <fqdn-key>/<podID> so that multiple Pods
// under the same FQDN each get their own sub-key, and CoreDNS returns all
// of them as a round-robin A response.
func (s *EtcdStore) Put(ctx context.Context, fqdn, podID, ip string) error {
	base, err := FQDNToEtcdKey(fqdn)
	if err != nil {
		return err
	}
	key := base + "/" + podID

	val, err := json.Marshal(skyDNSRecord{Host: ip})
	if err != nil {
		return fmt.Errorf("marshal record: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, defaultRequestTimeout)
	defer cancel()

	if _, err := s.client.Put(ctx, key, string(val)); err != nil {
		return fmt.Errorf("put etcd key %s: %w", key, err)
	}
	return nil
}

// Delete removes the A record for (fqdn, podID).
func (s *EtcdStore) Delete(ctx context.Context, fqdn, podID string) error {
	base, err := FQDNToEtcdKey(fqdn)
	if err != nil {
		return err
	}
	key := base + "/" + podID

	ctx, cancel := context.WithTimeout(ctx, defaultRequestTimeout)
	defer cancel()

	if _, err := s.client.Delete(ctx, key); err != nil {
		return fmt.Errorf("delete etcd key %s: %w", key, err)
	}
	return nil
}

// Get returns all IP addresses registered under fqdn.
// Returns an empty slice and no error if no records exist.
func (s *EtcdStore) Get(ctx context.Context, fqdn string) ([]string, error) {
	base, err := FQDNToEtcdKey(fqdn)
	if err != nil {
		return nil, err
	}
	prefix := base + "/"

	ctx, cancel := context.WithTimeout(ctx, defaultRequestTimeout)
	defer cancel()

	resp, err := s.client.Get(ctx, prefix, clientv3.WithPrefix())
	if err != nil {
		return nil, fmt.Errorf("get etcd prefix %s: %w", prefix, err)
	}

	var ips []string
	for _, kv := range resp.Kvs {
		var rec skyDNSRecord
		if err := json.Unmarshal(kv.Value, &rec); err != nil {
			return nil, fmt.Errorf("unmarshal record for %s: %w", string(kv.Key), err)
		}
		ips = append(ips, rec.Host)
	}
	return ips, nil
}

// Close closes the underlying etcd client.
func (s *EtcdStore) Close() error {
	return s.client.Close()
}

// FQDNToEtcdKey converts a vertexDomain FQDN to a SkyDNS-compatible etcd key.
//
// Example:
//
//	"vertex-in.pipeline1.default.vertexdomain.local"
//	→ "/skydns/local/vertexdomain/default/pipeline1/vertex-in"
func FQDNToEtcdKey(fqdn string) (string, error) {
	if fqdn == "" {
		return "", fmt.Errorf("fqdn must not be empty")
	}

	parts := strings.Split(fqdn, ".")
	if len(parts) < 2 {
		return "", fmt.Errorf("fqdn %q has too few labels", fqdn)
	}

	reversed := make([]string, len(parts))
	for i, p := range parts {
		reversed[len(parts)-1-i] = p
	}

	return skyDNSPrefix + "/" + strings.Join(reversed, "/"), nil
}
