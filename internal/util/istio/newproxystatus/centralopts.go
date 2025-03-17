// Copyright Istio Authors.
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

// NOTE: Code in this file was copied from istio code files and then modified so it can
// be used like a library. If major changes are made to istio it may break and need to be
// re-done. This is because the original code was tightly coupled to being called via cobra
// as part of the `istioctl proxy-status` CLI utility.
// Original code: https://github.com/istio/istio/blob/1.25.0/istioctl/pkg/clioptions/central.go#L27
package newproxystatus

import "time"

type CentralControlPlaneOptions struct {
	// Xds is XDS endpoint, e.g. localhost:15010.
	Xds string

	// XdsPodLabel is a Kubernetes label on the Istiod pods
	XdsPodLabel string

	// XdsPodPort is a port exposing XDS (typically 15010 or 15012)
	XdsPodPort int

	// CertDir is the local directory containing certificates
	CertDir string

	// Timeout is how long to wait before giving up on XDS
	Timeout time.Duration

	// InsecureSkipVerify skips client verification the server's certificate chain and host name.
	InsecureSkipVerify bool

	// XDSSAN is the expected Subject Alternative Name of the XDS server
	XDSSAN string

	// Plaintext forces plain text communication (for talking to port 15010)
	Plaintext bool

	// GCP project number or ID to use for XDS calls, if any.
	GCPProject string

	// Istiod address. For MCP may be different than Xds.
	IstiodAddr string
}
