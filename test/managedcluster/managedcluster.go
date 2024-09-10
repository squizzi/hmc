// Copyright 2024
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

package managedcluster

import (
	_ "embed"
	"fmt"
	"os"

	"github.com/a8m/envsubst"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v3"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type ProviderType string

const (
	ProviderCAPI  ProviderType = "cluster-api"
	ProviderAWS   ProviderType = "infrastructure-aws"
	ProviderAzure ProviderType = "infrastructure-azure"

	providerLabel = "cluster.x-k8s.io/provider"
)

type Template string

const (
	TemplateAWSStandaloneCP Template = "aws-standalone-cp"
	TemplateAWSHostedCP     Template = "aws-hosted-cp"
)

//go:embed resources/aws-standalone-cp.yaml.tpl
var awsStandaloneCPManagedClusterTemplateBytes []byte

//go:embed resources/aws-hosted-cp.yaml.tpl
var awsHostedCPManagedClusterTemplateBytes []byte

func GetProviderLabel(provider ProviderType) string {
	return fmt.Sprintf("%s=%s", providerLabel, provider)
}

// GetUnstructured returns an unstructured ManagedCluster object based on the
// provider and template.
func GetUnstructured(provider ProviderType, templateName Template) *unstructured.Unstructured {
	GinkgoHelper()

	generatedName := os.Getenv(EnvVarManagedClusterName)
	if generatedName == "" {
		generatedName = uuid.New().String()[:8] + "-e2e-test"
		_, _ = fmt.Fprintf(GinkgoWriter, "Generated cluster name: %q\n", generatedName)
		GinkgoT().Setenv(EnvVarManagedClusterName, generatedName)
	} else {
		_, _ = fmt.Fprintf(GinkgoWriter, "Using configured cluster name: %q\n", generatedName)
	}

	switch provider {
	case ProviderAWS:
		var managedClusterTemplateBytes []byte
		switch templateName {
		case TemplateAWSStandaloneCP:
			managedClusterTemplateBytes = awsStandaloneCPManagedClusterTemplateBytes
		case TemplateAWSHostedCP:
			hostedName := generatedName + "-hosted"

			GinkgoT().Setenv(EnvVarHostedManagedClusterName, hostedName)
			_, _ = fmt.Fprintf(GinkgoWriter, "Creating hosted ManagedCluster with name: %q\n", hostedName)

			// Validate environment vars that do not have defaults are populated.
			validateDeploymentVars([]string{
				EnvVarAWSVPCID,
				EnvVarAWSSubnetID,
				EnvVarAWSSubnetAvailabilityZone,
				EnvVarAWSSecurityGroupID,
			})

			managedClusterTemplateBytes = awsHostedCPManagedClusterTemplateBytes
		default:
			Fail(fmt.Sprintf("unsupported AWS template: %s", templateName))
		}

		managedClusterConfigBytes, err := envsubst.Bytes(managedClusterTemplateBytes)
		Expect(err).NotTo(HaveOccurred(), "failed to substitute environment variables")

		var managedClusterConfig map[string]interface{}

		err = yaml.Unmarshal(managedClusterConfigBytes, &managedClusterConfig)
		Expect(err).NotTo(HaveOccurred(), "failed to unmarshal deployment config")

		return &unstructured.Unstructured{Object: managedClusterConfig}
	default:
		Fail(fmt.Sprintf("unsupported provider: %s", provider))
	}

	return nil
}

func validateDeploymentVars(v []string) {
	GinkgoHelper()

	for _, envVar := range v {
		Expect(os.Getenv(envVar)).NotTo(BeEmpty(), envVar+" must be set")
	}
}
