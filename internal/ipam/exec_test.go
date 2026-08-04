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

package ipam

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// writeStubCNIBinary writes an executable shell script that stands in for
// the real whereabouts binary. It records CNI_* env vars and stdin to
// envFile/stdinFile so the test can assert what WhereaboutsExecutor sent,
// and prints stdout (or exits non-zero) as instructed by the test case.
func writeStubCNIBinary(t *testing.T, dir, stdout string, exitCode int) (binPath, envFile, stdinFile string) {
	t.Helper()
	binPath = filepath.Join(dir, "whereabouts-stub")
	envFile = filepath.Join(dir, "env.out")
	stdinFile = filepath.Join(dir, "stdin.out")

	script := "#!/bin/sh\n" +
		"env | grep '^CNI_' > " + envFile + "\n" +
		"cat > " + stdinFile + "\n"
	if exitCode != 0 {
		script += "exit " + strconv.Itoa(exitCode) + "\n"
	} else {
		script += "printf '%s' '" + stdout + "'\n"
	}

	if err := os.WriteFile(binPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write stub binary: %v", err)
	}
	return binPath, envFile, stdinFile
}

func TestWhereaboutsExecutor_Add(t *testing.T) {
	dir := t.TempDir()
	binPath, envFile, stdinFile := writeStubCNIBinary(t, dir,
		`{"ips":[{"address":"192.168.10.5/24"}]}`, 0)

	executor := &WhereaboutsExecutor{BinPath: binPath, CNIPath: "/opt/cni/bin"}
	conf := []byte(`{"cniVersion":"0.3.1","name":"test-net","ipam":{"type":"whereabouts","range":"192.168.10.0/24"}}`)

	ip, err := executor.Add(context.Background(), conf, "claim-abc-123")
	if err != nil {
		t.Fatalf("Add returned error: %v", err)
	}
	if ip.String() != "192.168.10.5" {
		t.Errorf("Add() IP = %q, want 192.168.10.5", ip.String())
	}

	envBytes, err := os.ReadFile(envFile)
	if err != nil {
		t.Fatalf("read captured env: %v", err)
	}
	env := string(envBytes)

	// The env vars asserted here are the exact CNI contract WhereaboutsExecutor
	// must uphold. CNI_ARGS regressed silently once (K8S_POD_NAME missing
	// caused whereabouts to fail with "no pod name" only in real k3d E2E,
	// undetected by unit tests) — this test pins the contract going forward.
	for _, want := range []string{
		"CNI_COMMAND=ADD",
		"CNI_CONTAINERID=claim-abc-123",
		"CNI_PATH=/opt/cni/bin",
		"CNI_ARGS=IgnoreUnknown=1;K8S_POD_NAME=claim-abc-123;K8S_POD_NAMESPACE=dranet-webhook",
	} {
		if !strings.Contains(env, want) {
			t.Errorf("captured env missing %q, got:\n%s", want, env)
		}
	}

	stdinBytes, err := os.ReadFile(stdinFile)
	if err != nil {
		t.Fatalf("read captured stdin: %v", err)
	}
	if string(stdinBytes) != string(conf) {
		t.Errorf("stdin = %q, want %q", stdinBytes, conf)
	}
}

func TestWhereaboutsExecutor_Add_NoIPsReturned(t *testing.T) {
	dir := t.TempDir()
	binPath, _, _ := writeStubCNIBinary(t, dir, `{"ips":[]}`, 0)

	executor := &WhereaboutsExecutor{BinPath: binPath, CNIPath: "/opt/cni/bin"}
	_, err := executor.Add(context.Background(), []byte(`{}`), "claim-1")
	if err == nil {
		t.Fatal("expected error when whereabouts returns no IPs, got nil")
	}
}

func TestWhereaboutsExecutor_Add_BinaryFails(t *testing.T) {
	dir := t.TempDir()
	binPath, _, _ := writeStubCNIBinary(t, dir, "", 1)

	executor := &WhereaboutsExecutor{BinPath: binPath, CNIPath: "/opt/cni/bin"}
	_, err := executor.Add(context.Background(), []byte(`{}`), "claim-1")
	if err == nil {
		t.Fatal("expected error when whereabouts binary exits non-zero, got nil")
	}
}

func TestWhereaboutsExecutor_Del(t *testing.T) {
	dir := t.TempDir()
	binPath, envFile, _ := writeStubCNIBinary(t, dir, "", 0)

	executor := &WhereaboutsExecutor{BinPath: binPath, CNIPath: "/opt/cni/bin"}
	if err := executor.Del(context.Background(), []byte(`{}`), "claim-xyz"); err != nil {
		t.Fatalf("Del returned error: %v", err)
	}

	env, err := os.ReadFile(envFile)
	if err != nil {
		t.Fatalf("read captured env: %v", err)
	}
	if !strings.Contains(string(env), "CNI_COMMAND=DEL") {
		t.Errorf("captured env missing CNI_COMMAND=DEL, got:\n%s", env)
	}
	if !strings.Contains(string(env), "CNI_CONTAINERID=claim-xyz") {
		t.Errorf("captured env missing CNI_CONTAINERID=claim-xyz, got:\n%s", env)
	}
}

func TestWhereaboutsExecutor_Del_BinaryFails(t *testing.T) {
	dir := t.TempDir()
	binPath, _, _ := writeStubCNIBinary(t, dir, "", 1)

	executor := &WhereaboutsExecutor{BinPath: binPath, CNIPath: "/opt/cni/bin"}
	if err := executor.Del(context.Background(), []byte(`{}`), "claim-1"); err == nil {
		t.Fatal("expected error when whereabouts binary exits non-zero, got nil")
	}
}
