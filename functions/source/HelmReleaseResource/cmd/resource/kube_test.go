package resource

import (
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/api/extensions/v1beta1"
	networkingv1beta1 "k8s.io/api/networking/v1beta1"
	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	"os"
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/stretchr/testify/assert"
)

// TestCreateKubeConfig to test createKubeConfig
func TestCreateKubeConfig(t *testing.T) {
	defer os.Remove(KubeConfigLocalPath)
	mockEKSSvc := &mockEKSClient{}
	mockSTSSvc := &mockSTSClient{}
	mockSMSvc := &mockSecretsManagerClient{}
	tests := map[string]struct {
		cluster, kubeconfig, role *string
		customKubeconfig          []byte
		expectedErr               string
	}{
		"AllValues": {
			cluster:     aws.String("eks"),
			kubeconfig:  aws.String("arn:aws:secretsmanager:us-east-2:1234567890:secret:kubeconfig-Wt"),
			role:        aws.String("arn:aws:iam::1234567890:role/TestRole"),
			expectedErr: "both ClusterID or KubeConfig can not be specified",
		},
		"OnlyCluster": {
			cluster:     aws.String("eks"),
			expectedErr: "",
		},
		"ClusterWithRole": {
			cluster:     aws.String("eks"),
			role:        aws.String("arn:aws:iam::1234567890:role/TestRole"),
			expectedErr: "",
		},
		"OnlySM": {
			kubeconfig:  aws.String("arn:aws:secretsmanager:us-east-2:1234567890:secret:kubeconfig-Wt"),
			expectedErr: "",
		},
		"NilValues": {
			expectedErr: "either ClusterID or KubeConfig must be specified",
		},
		"CustomKubeconfig": {
			customKubeconfig: []byte("Test"),
			expectedErr:      "",
		},
	}
	for name, d := range tests {
		t.Run(name, func(t *testing.T) {
			err := createKubeConfig(mockEKSSvc, mockSTSSvc, mockSMSvc, d.cluster, d.kubeconfig, d.customKubeconfig)
			if err != nil {
				assert.Contains(t, err.Error(), d.expectedErr)
			} else {
				assert.FileExists(t, KubeConfigLocalPath)
			}
		})
	}
}

// TestCreateNamespace to test createNamespace
func TestCreateNamespace(t *testing.T) {
	c := NewMockClient(t, nil)
	err := c.createNamespace("test")
	assert.NoError(t, err)
}

// TestCheckPendingResources to test CheckPendingResources
func TestCheckPendingResources(t *testing.T) {
	defer os.Remove(TempManifest)
	c := NewMockClient(t, nil)
	rd := &ReleaseData{
		Name:      "test",
		Namespace: "default",
	}
	tests := map[string]struct {
		assertion assert.BoolAssertionFunc
		manifest  string
	}{
		"Pending": {
			assertion: assert.True,
			manifest:  TestPendingManifest,
		},
		"NoPending": {
			assertion: assert.False,
			manifest:  TestManifest,
		},
	}
	for name, d := range tests {
		t.Run(name, func(t *testing.T) {
			rd.Manifest = d.manifest
			result, err := c.CheckPendingResources(rd)
			assert.Nil(t, err)
			d.assertion(t, result)
		})
	}
}

// TestGetKubeResources to test GetKubeResources
func TestGetKubeResources(t *testing.T) {
	defer os.Remove(TempManifest)
	c := NewMockClient(t, nil)
	manifest := `---
apiVersion: apps/v1
kind: Deployment
metadata:
 name: nginx-deployment

---
apiVersion: v1
kind: Service
metadata:
 name: my-service

---
apiVersion: v1
kind: Service
metadata:
 name: lb-service
 spec:
  type: LoadBalancer`
	expectedMap := map[string]interface{}{
		"Deployment": map[string]interface{}{
			"nginx-deployment": map[string]interface{}{
				"Namespace": "default", "Spec": interface{}(nil), "Status": map[string]interface{}{
					"ReadyReplicas": "1",
				},
			},
		}, "Service": map[string]interface{}{
			"lb-service": map[string]interface{}{
				"Namespace": "default", "Spec": map[string]interface{}{
					"ClusterIP": "127.0.0.1", "Type": "LoadBalancer",
				}, "Status": map[string]interface{}{
					"LoadBalancer": map[string]interface{}{
						"Ingress": []interface{}{
							map[string]interface{}{
								"Hostname": "elb.test.com",
							},
						},
					},
				},
			}, "my-service": map[string]interface{}{
				"Namespace": "default", "Spec": map[string]interface{}{
					"ClusterIP": "127.0.0.1", "Type": "ClusterIP",
				}, "Status": interface{}(nil),
			},
		},
	}
	rd := &ReleaseData{
		Name:      "test",
		Namespace: "default",
		Manifest:  manifest,
	}
	result, err := c.GetKubeResources(rd)
	assert.Nil(t, err)
	assert.EqualValues(t, expectedMap, result)
}

// TestGetManifestDetails to test getManifestDetails
func TestGetManifestDetails(t *testing.T) {
	defer os.Remove(TempManifest)
	c := NewMockClient(t, nil)
	rd := &ReleaseData{
		Name:      "test",
		Namespace: "default",
		Manifest:  TestManifest,
	}
	_, err := c.getManifestDetails(rd)
	assert.Nil(t, err)
}

// TestReady to test ingressReady, volumeReady and deploymentReady
func TestReady(t *testing.T) {
	tests := map[string]struct {
		assertion assert.BoolAssertionFunc
		ing       *v1beta1.Ingress
		ingN      *networkingv1beta1.Ingress
		pvc       *corev1.PersistentVolumeClaim
		dep       *appsv1.Deployment
	}{
		"Pending": {
			assertion: assert.False,
			ing:       ing("test-ingress", "default", true),
			ingN:      ingN("test-ingressN", "default", true),
			pvc:       vol("test-pvc", "default", true),
			dep:       dep("test-dep", "default", true),
		},
		"NoPending": {
			assertion: assert.True,
			ing:       ing("test-ingress", "default", false),
			ingN:      ingN("test-ingressN", "default", false),
			pvc:       vol("test-pvc", "default", false),
			dep:       dep("test-dep", "default", false),
		},
	}
	for name, d := range tests {
		t.Run(name, func(t *testing.T) {
			result := ingressReady(d.ing)
			d.assertion(t, result)
			result = ingressNReady(d.ingN)
			d.assertion(t, result)
			result = volumeReady(d.pvc)
			d.assertion(t, result)
			result = deploymentReady(d.dep)
			d.assertion(t, result)
		})
	}
}

// TestDaemonSetReadyReady to test daemonSetReady
func TestDaemonSetReadyReady(t *testing.T) {
	tests := map[string]struct {
		assertion assert.BoolAssertionFunc
		ds        *appsv1.DaemonSet
	}{
		"Pending": {
			assertion: assert.False,
			ds:        ds("test-ingress", "default", appsv1.RollingUpdateDaemonSetStrategyType, true),
		},
		"NoPending": {
			assertion: assert.True,
			ds:        ds("test-ingress", "default", appsv1.RollingUpdateDaemonSetStrategyType, false),
		},
		"OnDeleteStrategy": {
			assertion: assert.True,
			ds:        ds("test-ingress", "default", appsv1.OnDeleteDaemonSetStrategyType, false),
		},
	}
	for name, d := range tests {
		t.Run(name, func(t *testing.T) {
			result := daemonSetReady(d.ds)
			d.assertion(t, result)
		})
	}
}

// TestStatefulSetReady to test statefulSetReady
func TestStatefulSetReady(t *testing.T) {
	tests := map[string]struct {
		assertion assert.BoolAssertionFunc
		ss        *appsv1.StatefulSet
	}{
		"Pending": {
			assertion: assert.False,
			ss:        ss("test-ingress", "default", appsv1.RollingUpdateStatefulSetStrategyType, true),
		},
		"NoPending": {
			assertion: assert.True,
			ss:        ss("test-ingress", "default", appsv1.RollingUpdateStatefulSetStrategyType, false),
		},
		"OnDeleteStrategy": {
			assertion: assert.True,
			ss:        ss("test-ingress", "default", appsv1.OnDeleteStatefulSetStrategyType, false),
		},
	}
	for name, d := range tests {
		t.Run(name, func(t *testing.T) {
			result := statefulSetReady(d.ss)
			d.assertion(t, result)
		})
	}
}

func TestCrdReady(t *testing.T) {
	tests := map[string]struct {
		assertion assert.BoolAssertionFunc
		crd       *apiextv1.CustomResourceDefinition
	}{
		"Pending": {
			assertion: assert.False,
			crd:       crd("test-crd", "default", false, true),
		},
		"NoPending": {
			assertion: assert.True,
			crd:       crd("test-crd", "default", false, false),
		},
		"PendingWithNames": {
			assertion: assert.False,
			crd:       crd("test-crd", "default", true, true),
		},
		"NoPendingWithNames": {
			assertion: assert.True,
			crd:       crd("test-crd", "default", true, false),
		},
	}
	for name, d := range tests {
		t.Run(name, func(t *testing.T) {
			result := crdReady(d.crd)
			d.assertion(t, result)
		})
	}
}

func TestCrdBetaReady(t *testing.T) {
	tests := map[string]struct {
		assertion assert.BoolAssertionFunc
		crd       *apiextv1beta1.CustomResourceDefinition
	}{
		"Pending": {
			assertion: assert.False,
			crd:       crdBeta("test-crd", "default", false, true),
		},
		"NoPending": {
			assertion: assert.True,
			crd:       crdBeta("test-crd", "default", false, false),
		},
		"PendingWithNames": {
			assertion: assert.False,
			crd:       crdBeta("test-crd", "default", true, true),
		},
		"NoPendingWithNames": {
			assertion: assert.True,
			crd:       crdBeta("test-crd", "default", true, false),
		},
	}
	for name, d := range tests {
		t.Run(name, func(t *testing.T) {
			result := crdBetaReady(d.crd)
			d.assertion(t, result)
		})
	}
}
