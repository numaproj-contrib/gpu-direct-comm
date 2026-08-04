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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os/exec"
	"regexp"
)

// validClaimUID matches the characters permitted in a claim UID before it is
// embedded in the semicolon-delimited CNI_ARGS value. Without this check, a
// claimUID containing ';' or '=' could inject additional CNI_ARGS key/value
// pairs that whereabouts parses (e.g. overriding K8S_POD_NAMESPACE).
var validClaimUID = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// CNIExecutor abstracts the whereabouts CNI binary execution so that tests
// can substitute a fake without touching the filesystem or running real binaries.
type CNIExecutor interface {
	Add(ctx context.Context, conf []byte, claimUID string) (net.IP, error)
	Del(ctx context.Context, conf []byte, claimUID string) error
}

// cniAddResult is the subset of the CNI ADD result we need (IP allocation).
type cniAddResult struct {
	IPs []struct {
		Address string `json:"address"`
	} `json:"ips"`
}

// WhereaboutsExecutor executes the whereabouts CNI binary via os/exec.
type WhereaboutsExecutor struct {
	BinPath string
	CNIPath string
}

func (w *WhereaboutsExecutor) Add(ctx context.Context, conf []byte, claimUID string) (net.IP, error) {
	stdout, err := w.run(ctx, "ADD", conf, claimUID)
	if err != nil {
		return nil, fmt.Errorf("whereabouts ADD: %w", err)
	}

	var result cniAddResult
	if err := json.Unmarshal(stdout, &result); err != nil {
		return nil, fmt.Errorf("parse whereabouts ADD result: %w", err)
	}
	if len(result.IPs) == 0 {
		return nil, fmt.Errorf("whereabouts ADD returned no IPs")
	}

	ip, _, err := net.ParseCIDR(result.IPs[0].Address)
	if err != nil {
		ip = net.ParseIP(result.IPs[0].Address)
		if ip == nil {
			return nil, fmt.Errorf("parse allocated IP %q: %w", result.IPs[0].Address, err)
		}
	}
	return ip, nil
}

func (w *WhereaboutsExecutor) Del(ctx context.Context, conf []byte, claimUID string) error {
	if _, err := w.run(ctx, "DEL", conf, claimUID); err != nil {
		return fmt.Errorf("whereabouts DEL: %w", err)
	}
	return nil
}

func (w *WhereaboutsExecutor) run(ctx context.Context, command string, conf []byte, claimUID string) ([]byte, error) {
	if !validClaimUID.MatchString(claimUID) {
		return nil, fmt.Errorf("invalid claim UID %q: must match %s", claimUID, validClaimUID.String())
	}

	cmd := exec.CommandContext(ctx, w.BinPath)
	cmd.Stdin = bytes.NewReader(conf)
	cmd.Env = []string{
		"CNI_COMMAND=" + command,
		"CNI_CONTAINERID=" + claimUID,
		"CNI_NETNS=/dev/null",
		"CNI_IFNAME=eth0",
		"CNI_PATH=" + w.CNIPath,
		// whereabouts requires a non-empty K8S_POD_NAME to tag the IP
		// reservation's ownership. dranet's ProfileRequest carries no pod
		// identity (only claim_uid), so we use claimUID as a stand-in;
		// orphan reconciliation by pod identity is out of scope (ADR-0008).
		"CNI_ARGS=IgnoreUnknown=1;K8S_POD_NAME=" + claimUID + ";K8S_POD_NAMESPACE=dranet-webhook",
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%s (stderr: %s): %w", command, stderr.String(), err)
	}
	return stdout.Bytes(), nil
}
