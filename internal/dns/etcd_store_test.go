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
	"slices"
	"testing"
)

func TestFQDNToEtcdKey(t *testing.T) {
	tests := []struct {
		name    string
		fqdn    string
		want    string
		wantErr bool
	}{
		{
			name: "standard vertexdomain FQDN",
			fqdn: "vertex-in.pipeline1.default.vertexdomain.local",
			want: "/skydns/local/vertexdomain/default/pipeline1/vertex-in",
		},
		{
			name: "different namespace and pipeline",
			fqdn: "filter-resize.my-pipeline.my-namespace.vertexdomain.local",
			want: "/skydns/local/vertexdomain/my-namespace/my-pipeline/filter-resize",
		},
		{
			name: "two labels only",
			fqdn: "vertexdomain.local",
			want: "/skydns/local/vertexdomain",
		},
		{
			name:    "empty FQDN",
			fqdn:    "",
			wantErr: true,
		},
		{
			name:    "single label",
			fqdn:    "local",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := FQDNToEtcdKey(tc.fqdn)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got key %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("FQDNToEtcdKey(%q) = %q, want %q", tc.fqdn, got, tc.want)
			}
		})
	}
}

func TestSkyDNSRecordJSON(t *testing.T) {
	tests := []struct {
		name string
		ip   string
		want string
	}{
		{
			name: "IPv4 address",
			ip:   "192.168.140.10",
			want: `{"host":"192.168.140.10"}`,
		},
		{
			name: "IPv6 address",
			ip:   "fd00::1",
			want: `{"host":"fd00::1"}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := skyDNSRecord{Host: tc.ip}
			got, err := json.Marshal(rec)
			if err != nil {
				t.Fatalf("unexpected marshal error: %v", err)
			}
			if string(got) != tc.want {
				t.Errorf("json.Marshal(%+v) = %s, want %s", rec, got, tc.want)
			}

			var roundtrip skyDNSRecord
			if err := json.Unmarshal(got, &roundtrip); err != nil {
				t.Fatalf("unexpected unmarshal error: %v", err)
			}
			if roundtrip.Host != tc.ip {
				t.Errorf("roundtrip Host = %q, want %q", roundtrip.Host, tc.ip)
			}
		})
	}
}

func TestEtcdStorePutGetDelete(t *testing.T) {
	client := newEmbeddedEtcdClient(t)
	store := NewEtcdStoreFromClient(client)
	ctx := context.Background()

	fqdn := "vertex-in.pipeline1.default.vertexdomain.local"
	podID := "pipeline1-vertex-in-0"
	ip := "192.168.140.10"

	// Get on a non-existent record returns empty slice.
	got, err := store.Get(ctx, fqdn)
	if err != nil {
		t.Fatalf("Get before Put: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("Get before Put = %v, want empty", got)
	}

	// Put a record.
	if err := store.Put(ctx, fqdn, podID, ip); err != nil {
		t.Fatalf("Put: %v", err)
	}

	// Get returns the stored IP.
	got, err = store.Get(ctx, fqdn)
	if err != nil {
		t.Fatalf("Get after Put: %v", err)
	}
	if len(got) != 1 || got[0] != ip {
		t.Errorf("Get after Put = %v, want [%s]", got, ip)
	}

	// Put with same podID overwrites the IP.
	newIP := "192.168.140.20"
	if err := store.Put(ctx, fqdn, podID, newIP); err != nil {
		t.Fatalf("Put overwrite: %v", err)
	}
	got, err = store.Get(ctx, fqdn)
	if err != nil {
		t.Fatalf("Get after overwrite: %v", err)
	}
	if len(got) != 1 || got[0] != newIP {
		t.Errorf("Get after overwrite = %v, want [%s]", got, newIP)
	}

	// Delete removes the record.
	if err := store.Delete(ctx, fqdn, podID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	got, err = store.Get(ctx, fqdn)
	if err != nil {
		t.Fatalf("Get after Delete: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("Get after Delete = %v, want empty", got)
	}

	// Delete on a non-existent key does not error.
	if err := store.Delete(ctx, fqdn, podID); err != nil {
		t.Fatalf("Delete non-existent: %v", err)
	}
}

func TestEtcdStoreMultiplePods(t *testing.T) {
	client := newEmbeddedEtcdClient(t)
	store := NewEtcdStoreFromClient(client)
	ctx := context.Background()

	fqdn := "vertex-in.pipeline1.default.vertexdomain.local"
	pods := []struct {
		id string
		ip string
	}{
		{"pipeline1-vertex-in-0", "192.168.140.10"},
		{"pipeline1-vertex-in-1", "192.168.140.11"},
		{"pipeline1-vertex-in-2", "192.168.140.12"},
	}

	for _, p := range pods {
		if err := store.Put(ctx, fqdn, p.id, p.ip); err != nil {
			t.Fatalf("Put %s: %v", p.id, err)
		}
	}

	got, err := store.Get(ctx, fqdn)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("Get returned %d IPs, want 3", len(got))
	}

	wantIPs := []string{"192.168.140.10", "192.168.140.11", "192.168.140.12"}
	slices.Sort(got)
	slices.Sort(wantIPs)
	for i := range wantIPs {
		if got[i] != wantIPs[i] {
			t.Errorf("got[%d] = %q, want %q", i, got[i], wantIPs[i])
		}
	}
}

func TestEtcdStoreDeleteSinglePod(t *testing.T) {
	client := newEmbeddedEtcdClient(t)
	store := NewEtcdStoreFromClient(client)
	ctx := context.Background()

	fqdn := "vertex-in.pipeline1.default.vertexdomain.local"

	if err := store.Put(ctx, fqdn, "pod-0", "10.0.0.1"); err != nil {
		t.Fatalf("Put pod-0: %v", err)
	}
	if err := store.Put(ctx, fqdn, "pod-1", "10.0.0.2"); err != nil {
		t.Fatalf("Put pod-1: %v", err)
	}
	if err := store.Put(ctx, fqdn, "pod-2", "10.0.0.3"); err != nil {
		t.Fatalf("Put pod-2: %v", err)
	}

	// Delete only pod-1.
	if err := store.Delete(ctx, fqdn, "pod-1"); err != nil {
		t.Fatalf("Delete pod-1: %v", err)
	}

	got, err := store.Get(ctx, fqdn)
	if err != nil {
		t.Fatalf("Get after single delete: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("Get returned %d IPs, want 2", len(got))
	}

	slices.Sort(got)
	want := []string{"10.0.0.1", "10.0.0.3"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestEtcdStorePutInvalidFQDN(t *testing.T) {
	client := newEmbeddedEtcdClient(t)
	store := NewEtcdStoreFromClient(client)
	ctx := context.Background()

	if err := store.Put(ctx, "", "pod-0", "192.168.140.10"); err == nil {
		t.Fatal("Put with empty FQDN should return error")
	}

	if err := store.Delete(ctx, "", "pod-0"); err == nil {
		t.Fatal("Delete with empty FQDN should return error")
	}

	if _, err := store.Get(ctx, ""); err == nil {
		t.Fatal("Get with empty FQDN should return error")
	}
}
