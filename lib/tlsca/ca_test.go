/*
Copyright 2019 Gravitational, Inc.

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

package tlsca

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509/pkix"
	"encoding/asn1"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/gravitational/trace"
	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gravitational/teleport"
	"github.com/gravitational/teleport/api/constants"
	"github.com/gravitational/teleport/lib/fixtures"
)

// TestPrincipals makes sure that SAN extension of generated x509 cert gets
// correctly set with DNS names and IP addresses based on the provided
// principals.
func TestPrincipals(t *testing.T) {
	tests := []struct {
		name       string
		createFunc func() (*CertAuthority, error)
	}{
		{
			name: "FromKeys",
			createFunc: func() (*CertAuthority, error) {
				return FromKeys([]byte(fixtures.TLSCACertPEM), []byte(fixtures.TLSCAKeyPEM))
			},
		},
		{
			name: "FromCertAndSigner",
			createFunc: func() (*CertAuthority, error) {
				signer, err := ParsePrivateKeyPEM([]byte(fixtures.TLSCAKeyPEM))
				if err != nil {
					return nil, trace.Wrap(err)
				}
				return FromCertAndSigner([]byte(fixtures.TLSCACertPEM), signer)
			},
		},
		{
			name: "FromTLSCertificate",
			createFunc: func() (*CertAuthority, error) {
				cert, err := tls.X509KeyPair([]byte(fixtures.TLSCACertPEM), []byte(fixtures.TLSCAKeyPEM))
				if err != nil {
					return nil, trace.Wrap(err)
				}
				return FromTLSCertificate(cert)
			},
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			ca, err := test.createFunc()
			require.NoError(t, err)

			privateKey, err := rsa.GenerateKey(rand.Reader, constants.RSAKeySize)
			require.NoError(t, err)

			hostnames := []string{"localhost", "example.com"}
			ips := []string{"127.0.0.1", "192.168.1.1"}

			clock := clockwork.NewFakeClock()

			certBytes, err := ca.GenerateCertificate(CertificateRequest{
				Clock:     clock,
				PublicKey: privateKey.Public(),
				Subject:   pkix.Name{CommonName: "test"},
				NotAfter:  clock.Now().Add(time.Hour),
				DNSNames:  append(hostnames, ips...),
			})
			require.NoError(t, err)

			cert, err := ParseCertificatePEM(certBytes)
			require.NoError(t, err)
			require.ElementsMatch(t, cert.DNSNames, hostnames)
			var certIPs []string
			for _, ip := range cert.IPAddresses {
				certIPs = append(certIPs, ip.String())
			}
			require.ElementsMatch(t, certIPs, ips)
		})
	}
}

func TestRenewableIdentity(t *testing.T) {
	clock := clockwork.NewFakeClock()
	expires := clock.Now().Add(1 * time.Hour)

	ca, err := FromKeys([]byte(fixtures.TLSCACertPEM), []byte(fixtures.TLSCAKeyPEM))
	require.NoError(t, err)

	privateKey, err := rsa.GenerateKey(rand.Reader, constants.RSAKeySize)
	require.NoError(t, err)

	identity := Identity{
		Username:  "alice@example.com",
		Groups:    []string{"admin"},
		Expires:   expires,
		Renewable: true,
	}

	subj, err := identity.Subject()
	require.NoError(t, err)
	require.NotNil(t, subj)

	certBytes, err := ca.GenerateCertificate(CertificateRequest{
		Clock:     clock,
		PublicKey: privateKey.Public(),
		Subject:   subj,
		NotAfter:  expires,
	})
	require.NoError(t, err)

	cert, err := ParseCertificatePEM(certBytes)
	require.NoError(t, err)

	parsed, err := FromSubject(cert.Subject, expires)
	require.NoError(t, err)
	require.NotNil(t, parsed)
	require.True(t, parsed.Renewable)
}

// TestKubeExtensions test ASN1 subject kubernetes extensions
func TestKubeExtensions(t *testing.T) {
	clock := clockwork.NewFakeClock()
	ca, err := FromKeys([]byte(fixtures.TLSCACertPEM), []byte(fixtures.TLSCAKeyPEM))
	require.NoError(t, err)

	privateKey, err := rsa.GenerateKey(rand.Reader, constants.RSAKeySize)
	require.NoError(t, err)

	expires := clock.Now().Add(time.Hour)
	identity := Identity{
		Username:     "alice@example.com",
		Groups:       []string{"admin"},
		Impersonator: "bob@example.com",
		// Generate a certificate restricted for
		// use against a kubernetes endpoint, and not the API server endpoint
		// otherwise proxies can generate certs for any user.
		Usage:             []string{teleport.UsageKubeOnly},
		KubernetesGroups:  []string{"system:masters", "admin"},
		KubernetesUsers:   []string{"IAM#alice@example.com"},
		KubernetesCluster: "kube-cluster",
		TeleportCluster:   "tele-cluster",
		RouteToDatabase: RouteToDatabase{
			ServiceName: "postgres-rds",
			Protocol:    "postgres",
			Username:    "postgres",
		},
		DatabaseNames: []string{"postgres", "main"},
		DatabaseUsers: []string{"postgres", "alice"},
		Expires:       expires,
	}

	subj, err := identity.Subject()
	require.NoError(t, err)

	certBytes, err := ca.GenerateCertificate(CertificateRequest{
		Clock:     clock,
		PublicKey: privateKey.Public(),
		Subject:   subj,
		NotAfter:  expires,
	})
	require.NoError(t, err)

	cert, err := ParseCertificatePEM(certBytes)
	require.NoError(t, err)
	out, err := FromSubject(cert.Subject, cert.NotAfter)
	require.NoError(t, err)
	require.False(t, out.Renewable)
	require.Empty(t, cmp.Diff(out, &identity))
}

func TestAzureExtensions(t *testing.T) {
	clock := clockwork.NewFakeClock()
	ca, err := FromKeys([]byte(fixtures.TLSCACertPEM), []byte(fixtures.TLSCAKeyPEM))
	require.NoError(t, err)

	privateKey, err := rsa.GenerateKey(rand.Reader, constants.RSAKeySize)
	require.NoError(t, err)

	expires := clock.Now().Add(time.Hour)
	identity := Identity{
		Username:        "alice@example.com",
		Groups:          []string{"admin"},
		Impersonator:    "bob@example.com",
		Usage:           []string{teleport.UsageAppsOnly},
		AzureIdentities: []string{"azure-identity-1", "azure-identity-2"},
		RouteToApp: RouteToApp{
			SessionID:     "43de4ffa8509aff3e3990e941400a403a12a6024d59897167b780ec0d03a1f15",
			ClusterName:   "teleport.example.com",
			Name:          "azure-app",
			AzureIdentity: "azure-identity-3",
		},
		TeleportCluster: "tele-cluster",
		Expires:         expires,
	}

	subj, err := identity.Subject()
	require.NoError(t, err)

	certBytes, err := ca.GenerateCertificate(CertificateRequest{
		Clock:     clock,
		PublicKey: privateKey.Public(),
		Subject:   subj,
		NotAfter:  expires,
	})
	require.NoError(t, err)

	cert, err := ParseCertificatePEM(certBytes)
	require.NoError(t, err)
	out, err := FromSubject(cert.Subject, cert.NotAfter)
	require.NoError(t, err)
	require.Empty(t, cmp.Diff(out, &identity))
	require.Equal(t, "43de4ffa8509aff3e3990e941400a403a12a6024d59897167b780ec0d03a1f15", out.RouteToApp.SessionID)
}

func TestIdentity_ToFromSubject(t *testing.T) {
	assertStringOID := func(t *testing.T, want string, oid asn1.ObjectIdentifier, subj *pkix.Name, msgAndArgs ...any) {
		for _, en := range subj.ExtraNames {
			if !oid.Equal(en.Type) {
				continue
			}

			got, ok := en.Value.(string)
			require.True(t, ok, "Value for OID %v is not a string: %T", oid, en.Value)
			assert.Equal(t, want, got, msgAndArgs)
			return
		}
		t.Fatalf("OID %v not found", oid)
	}

	tests := []struct {
		name          string
		identity      *Identity
		assertSubject func(t *testing.T, identity *Identity, subj *pkix.Name)
	}{
		{
			name: "device extensions",
			identity: &Identity{
				Username: "llama",                      // Required.
				Groups:   []string{"editor", "viewer"}, // Required.
				DeviceExtensions: DeviceExtensions{
					DeviceID:     "deviceid1",
					AssetTag:     "assettag2",
					CredentialID: "credentialid3",
				},
			},
			assertSubject: func(t *testing.T, identity *Identity, subj *pkix.Name) {
				want := identity.DeviceExtensions
				assertStringOID(t, want.DeviceID, DeviceIDExtensionOID, subj, "DeviceID mismatch")
				assertStringOID(t, want.AssetTag, DeviceAssetTagExtensionOID, subj, "AssetTag mismatch")
				assertStringOID(t, want.CredentialID, DeviceCredentialIDExtensionOID, subj, "CredentialID mismatch")
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			identity := test.identity

			// Marshal identity into subject.
			subj, err := identity.Subject()
			require.NoError(t, err, "Subject failed")
			test.assertSubject(t, identity, &subj)

			// ExtraNames are appended to Names when the cert is created.
			subj.Names = append(subj.Names, subj.ExtraNames...)
			subj.ExtraNames = nil

			// Extract identity from subject and verify that no data got lost.
			got, err := FromSubject(subj, identity.Expires)
			require.NoError(t, err, "FromSubject failed")
			if diff := cmp.Diff(identity, got, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("FromSubject mismatch (-want +got)\n%s", diff)
			}
		})
	}
}

func TestGCPExtensions(t *testing.T) {
	clock := clockwork.NewFakeClock()
	ca, err := FromKeys([]byte(fixtures.TLSCACertPEM), []byte(fixtures.TLSCAKeyPEM))
	require.NoError(t, err)

	privateKey, err := rsa.GenerateKey(rand.Reader, constants.RSAKeySize)
	require.NoError(t, err)

	expires := clock.Now().Add(time.Hour)
	identity := Identity{
		Username:           "alice@example.com",
		Groups:             []string{"admin"},
		Impersonator:       "bob@example.com",
		Usage:              []string{teleport.UsageAppsOnly},
		GCPServiceAccounts: []string{"acct-1@example-123456.iam.gserviceaccount.com", "acct-2@example-123456.iam.gserviceaccount.com"},
		RouteToApp: RouteToApp{
			SessionID:         "43de4ffa8509aff3e3990e941400a403a12a6024d59897167b780ec0d03a1f15",
			ClusterName:       "teleport.example.com",
			Name:              "GCP-app",
			GCPServiceAccount: "acct-3@example-123456.iam.gserviceaccount.com",
		},
		TeleportCluster: "tele-cluster",
		Expires:         expires,
	}

	subj, err := identity.Subject()
	require.NoError(t, err)

	certBytes, err := ca.GenerateCertificate(CertificateRequest{
		Clock:     clock,
		PublicKey: privateKey.Public(),
		Subject:   subj,
		NotAfter:  expires,
	})
	require.NoError(t, err)

	cert, err := ParseCertificatePEM(certBytes)
	require.NoError(t, err)
	out, err := FromSubject(cert.Subject, cert.NotAfter)
	require.NoError(t, err)
	require.Empty(t, cmp.Diff(out, &identity))
}
