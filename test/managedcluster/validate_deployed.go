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
	"context"
	"fmt"
	"strings"

	"github.com/Mirantis/hmc/test/kubeclient"
	"github.com/Mirantis/hmc/test/utils"
	. "github.com/onsi/ginkgo/v2"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// resourceValidationFunc is intended to validate a specific kubernetes
// resource.
type resourceValidationFunc func(context.Context, *kubeclient.KubeClient, string) error

func NewDeployedValidation() map[string]resourceValidationFunc {
	return map[string]resourceValidationFunc{
		"clusters":       validateCluster,
		"machines":       validateMachines,
		"control-planes": validateK0sControlPlanes,
		"csi-driver":     validateCSIDriver,
		"ccm":            validateCCM,
	}
}

// VerifyProviderDeployed is a provider-agnostic verification that checks
// to ensure generic resources managed by the provider have been deleted.
// It is intended to be used in conjunction with an Eventually block.
func VerifyProviderDeployed(
	ctx context.Context, kc *kubeclient.KubeClient, clusterName string,
	resourceValidationMap map[string]resourceValidationFunc) error {
	return verifyProviderAction(ctx, kc, clusterName, resourceValidationMap,
		[]string{"clusters", "machines", "control-planes", "csi-driver", "ccm"})
}

// verifyProviderAction is a provider-agnostic verification that checks for
// a specific set of resources and either validates their readiness or
// their deletion depending on the passed map of resourceValidationFuncs and
// desired order.
// It is meant to be used in conjunction with an Eventually block.
// In some cases it may be necessary to end the Eventually block early if the
// resource will never reach a ready state, in these instances Ginkgo's Fail
// should be used to end the spec early.
func verifyProviderAction(
	ctx context.Context, kc *kubeclient.KubeClient, clusterName string,
	resourcesToValidate map[string]resourceValidationFunc, order []string) error {
	// Sequentially validate each resource type, only returning the first error
	// as to not move on to the next resource type until the first is resolved.
	// We use []string here since order is important.
	for _, name := range order {
		validator, ok := resourcesToValidate[name]
		if !ok {
			continue
		}

		if err := validator(ctx, kc, clusterName); err != nil {
			_, _ = fmt.Fprintf(GinkgoWriter, "[%s] validation error: %v\n", name, err)
			return err
		}

		_, _ = fmt.Fprintf(GinkgoWriter, "[%s] validation succeeded\n", name)
		delete(resourcesToValidate, name)
	}

	return nil
}

func validateCluster(ctx context.Context, kc *kubeclient.KubeClient, clusterName string) error {
	cluster, err := kc.GetCluster(ctx, clusterName)
	if err != nil {
		return err
	}

	phase, _, err := unstructured.NestedString(cluster.Object, "status", "phase")
	if err != nil {
		return fmt.Errorf("failed to get status.phase for %s: %v", cluster.GetName(), err)
	}

	if phase == "Deleting" {
		Fail(fmt.Sprintf("%s is in 'Deleting' phase", cluster.GetName()))
	}

	if err := utils.ValidateObjectNamePrefix(cluster, clusterName); err != nil {
		Fail(err.Error())
	}

	if err := utils.ValidateConditionsTrue(cluster); err != nil {
		return err
	}

	return nil
}

func validateMachines(ctx context.Context, kc *kubeclient.KubeClient, clusterName string) error {
	machines, err := kc.ListMachines(ctx, clusterName)
	if err != nil {
		return err
	}

	for _, machine := range machines {
		if err := utils.ValidateObjectNamePrefix(&machine, clusterName); err != nil {
			Fail(err.Error())
		}

		if err := utils.ValidateConditionsTrue(&machine); err != nil {
			return err
		}
	}

	return nil
}

func validateK0sControlPlanes(ctx context.Context, kc *kubeclient.KubeClient, clusterName string) error {
	controlPlanes, err := kc.ListK0sControlPlanes(ctx, clusterName)
	if err != nil {
		return err
	}

	for _, controlPlane := range controlPlanes {
		if err := utils.ValidateObjectNamePrefix(&controlPlane, clusterName); err != nil {
			Fail(err.Error())
		}

		objKind, objName := utils.ObjKindName(&controlPlane)

		// k0s does not use the metav1.Condition type for status.conditions,
		// instead it uses a custom type so we can't use
		// ValidateConditionsTrue here, instead we'll check for "ready: true".
		status, found, err := unstructured.NestedFieldCopy(controlPlane.Object, "status")
		if !found {
			return fmt.Errorf("no status found for %s: %s", objKind, objName)
		}
		if err != nil {
			return fmt.Errorf("failed to get status conditions for %s: %s: %w", objKind, objName, err)
		}

		st, ok := status.(map[string]interface{})
		if !ok {
			return fmt.Errorf("expected K0sControlPlane condition to be type map[string]interface{}, got: %T", status)
		}

		if _, ok := st["ready"]; !ok {
			return fmt.Errorf("%s %s has no 'ready' status", objKind, objName)
		}

		if !st["ready"].(bool) {
			return fmt.Errorf("%s %s is not ready, status: %+v", objKind, objName, st)
		}
	}

	return nil
}

// validateCSIDriver validates that the provider CSI driver is functioning
// by creating a PVC and verifying it enters "Bound" status.
func validateCSIDriver(ctx context.Context, kc *kubeclient.KubeClient, clusterName string) error {
	clusterKC := kc.NewFromCluster(ctx, "default", clusterName)

	pvcName := clusterName + "-csi-test-pvc"

	_, err := clusterKC.Client.CoreV1().PersistentVolumeClaims(clusterKC.Namespace).
		Create(ctx, &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name: pvcName,
			},
			Spec: corev1.PersistentVolumeClaimSpec{
				AccessModes: []corev1.PersistentVolumeAccessMode{
					corev1.ReadWriteOnce,
				},
				Resources: corev1.VolumeResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceStorage: resource.MustParse("1Gi"),
					},
				},
			},
		}, metav1.CreateOptions{})
	if err != nil {
		// Since these resourceValidationFuncs are intended to be used in
		// Eventually we should ensure a follow-up PVCreate is a no-op.
		if !apierrors.IsAlreadyExists(err) {
			Fail(fmt.Sprintf("failed to create test PVC: %v", err))
		}
	}

	// Create a pod that uses the PVC so that the PVC enters "Bound" status.
	_, err = clusterKC.Client.CoreV1().Pods(clusterKC.Namespace).Create(ctx, &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: pvcName + "-pod",
		},
		Spec: corev1.PodSpec{
			Volumes: []corev1.Volume{
				{
					Name: "test-pvc-vol",
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: pvcName,
						},
					},
				},
			},
			Containers: []corev1.Container{
				{
					Name:  "test-pvc-container",
					Image: "nginx",
					VolumeMounts: []corev1.VolumeMount{
						{
							MountPath: "/storage",
							Name:      "test-pvc-vol",
						},
					},
				},
			},
		},
	}, metav1.CreateOptions{})
	if err != nil {
		if !apierrors.IsAlreadyExists(err) {
			Fail(fmt.Sprintf("failed to create test Pod: %v", err))
		}
	}

	// Verify the PVC enters "Bound" status and inherits the CSI driver
	// storageClass without us having to specify it.
	pvc, err := clusterKC.Client.CoreV1().PersistentVolumeClaims(clusterKC.Namespace).
		Get(ctx, pvcName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get test PVC: %w", err)
	}

	if !strings.Contains(*pvc.Spec.StorageClassName, "csi") {
		Fail(fmt.Sprintf("%s PersistentVolumeClaim does not have a CSI driver storageClass", pvcName))
	}

	if pvc.Status.Phase != corev1.ClaimBound {
		return fmt.Errorf("%s PersistentVolume not yet 'Bound', current phase: %q", pvcName, pvc.Status.Phase)
	}

	return nil
}

// validateCCM validates that the provider's cloud controller manager is
// functional by creating a LoadBalancer service and verifying it is assigned
// an external IP.
func validateCCM(ctx context.Context, kc *kubeclient.KubeClient, clusterName string) error {
	clusterKC := kc.NewFromCluster(ctx, "default", clusterName)

	createdServiceName := "loadbalancer-" + clusterName

	_, err := clusterKC.Client.CoreV1().Services(clusterKC.Namespace).Create(ctx, &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: createdServiceName,
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				"some": "selector",
			},
			Ports: []corev1.ServicePort{
				{
					Port:       8765,
					TargetPort: intstr.FromInt(9376),
				},
			},
			Type: corev1.ServiceTypeLoadBalancer,
		},
	}, metav1.CreateOptions{})
	if err != nil {
		// Since these resourceValidationFuncs are intended to be used in
		// Eventually we should ensure a follow-up ServiceCreate is a no-op.
		if !apierrors.IsAlreadyExists(err) {
			return fmt.Errorf("failed to create test Service: %w", err)
		}
	}

	// Verify the Service is assigned an external IP.
	service, err := clusterKC.Client.CoreV1().Services(clusterKC.Namespace).
		Get(ctx, createdServiceName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get test Service: %w", err)
	}

	for _, i := range service.Status.LoadBalancer.Ingress {
		if i.Hostname != "" {
			return nil
		}
	}

	return fmt.Errorf("%s Service does not yet have an external hostname", service.Name)
}
