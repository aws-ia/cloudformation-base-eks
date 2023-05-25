package resource

import (
	"bytes"

	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/client/metadata"
	"github.com/aws/aws-sdk-go/aws/request"
	"github.com/aws/aws-sdk-go/aws/session"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chartutil"
	"helm.sh/helm/v3/pkg/cli"
	kubefake "helm.sh/helm/v3/pkg/kube/fake"
	"helm.sh/helm/v3/pkg/release"
	"helm.sh/helm/v3/pkg/storage"
	"helm.sh/helm/v3/pkg/storage/driver"
	htime "helm.sh/helm/v3/pkg/time"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	v1beta1 "k8s.io/api/extensions/v1beta1"
	networkingv1beta1 "k8s.io/api/networking/v1beta1"
	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/meta/testrestmapper"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/resource"
	"k8s.io/client-go/discovery"
	fakeclientset "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest/fake"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/kubectl/pkg/scheme"
)

type chartOptions struct {
	*chart.Chart
}

type chartOption func(*chartOptions)

type fakeCachedDiscoveryClient struct {
	discovery.DiscoveryInterface
}

var (
	TestFolder  = "testdata"
	TestZipFile = TestFolder + "/test_lambda.zip"
)

// Session is a mock session which is used to hit the mock server
var MockSession = func() *session.Session {
	// server is the mock server that simply writes a 200 status back to the client
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	return session.Must(session.NewSession(&aws.Config{
		DisableSSL: aws.Bool(true),
		Endpoint:   aws.String(server.URL),
		Region:     aws.String("us-east-1"),
	}))
}()

var TestManifest = `---
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
  type: LoadBalancer

---
apiVersion: apps/v1
kind: DaemonSet
metadata:
 name: nginx-ds

---
apiVersion: apps/v1
kind: StatefulSet
metadata:
 name: nginx-ss

---
apiVersion: networking.k8s.io/v1beta1
kind: Ingress
metadata:
  name: test-ingress`

var TestPendingManifest = `apiVersion: apps/v1
kind: Deployment
metadata:
 name: nginx-deployment-foo`

func newFakeBuilder(t *testing.T) func() *resource.Builder {
	cfg, _ := clientcmd.NewDefaultClientConfigLoadingRules().Load()
	clientConfig := clientcmd.NewDefaultClientConfig(*cfg, &clientcmd.ConfigOverrides{})
	configFlags := genericclioptions.NewTestConfigFlags().
		WithClientConfig(clientConfig).
		WithRESTMapper(testRESTMapper())
	header := http.Header{}
	header.Set("Content-Type", runtime.ContentTypeJSON)
	codec := scheme.Codecs.LegacyCodec(scheme.Scheme.PrioritizedVersionsAllGroups()...)
	return func() *resource.Builder {
		return resource.NewFakeBuilder(
			func(version schema.GroupVersion) (resource.RESTClient, error) {
				return &fake.RESTClient{
					GroupVersion:         schema.GroupVersion{Version: "v1"},
					NegotiatedSerializer: resource.UnstructuredPlusDefaultContentConfig().NegotiatedSerializer,
					Client: fake.CreateHTTPClient(func(req *http.Request) (*http.Response, error) {
						switch p, m := req.URL.Path, req.Method; {
						case p == "/namespaces/test/services" && m == "POST":
							return &http.Response{StatusCode: http.StatusCreated, Header: header, Body: ObjBody(codec, ns("test"))}, nil
						case p == "/namespaces/default/deployments/nginx-deployment" && m == "GET":
							return &http.Response{StatusCode: http.StatusOK, Header: header, Body: ObjBody(codec, dep("nginx-deployment", "default", false))}, nil
						case p == "/namespaces/default/deployments/nginx-deployment-foo" && m == "GET":
							return &http.Response{StatusCode: http.StatusOK, Header: header, Body: ObjBody(codec, dep("nginx-deployment-foo", "default", true))}, nil
						case p == "/namespaces/default/services/my-service" && m == "GET":
							return &http.Response{StatusCode: http.StatusOK, Header: header, Body: ObjBody(codec, svc("my-service", "default", v1.ServiceTypeClusterIP))}, nil
						case p == "/namespaces/default/services/lb-service" && m == "GET":
							return &http.Response{StatusCode: http.StatusOK, Header: header, Body: ObjBody(codec, svc("lb-service", "default", v1.ServiceTypeLoadBalancer))}, nil
						case p == "/namespaces/default/daemonsets/nginx-ds" && m == "GET":
							return &http.Response{StatusCode: http.StatusOK, Header: header, Body: ObjBody(codec, ds("nginx-ds", "default", appsv1.RollingUpdateDaemonSetStrategyType, false))}, nil
						case p == "/namespaces/default/statefulsets/nginx-ss" && m == "GET":
							return &http.Response{StatusCode: http.StatusOK, Header: header, Body: ObjBody(codec, ss("nginx-ss", "default", appsv1.RollingUpdateStatefulSetStrategyType, false))}, nil
						case p == "/namespaces/default/ingress/test-ingress" && m == "GET":
							return &http.Response{StatusCode: http.StatusOK, Header: header, Body: ObjBody(codec, ing("test-ingress", "default", false))}, nil
						default:
							t.Fatalf("unexpected request: %#v\n%#v", req.URL, req)
							return nil, nil
						}
					}),
				}, nil
			},
			configFlags.ToRESTMapper,
			func() (restmapper.CategoryExpander, error) {
				return resource.FakeCategoryExpander, nil
			},
		)
	}
}

type mockAWSClients struct {
	AWSSession *session.Session
	AWSClientsIface
}

func NewMockClient(t *testing.T, m *Model) *Clients {
	t.Helper()
	h := ActionConfigFixture(t)
	makeMeSomeReleases(h.Releases, t)
	c := &Clients{
		ResourceBuilder: newFakeBuilder(t),
		ClientSet: fakeclientset.NewSimpleClientset(
			dep("nginx-deployment", "default", false),
			dep("nginx-deployment-foo", "default", true),
			svc("my-service", "default", v1.ServiceTypeClusterIP),
			svc("lb-service", "default", v1.ServiceTypeLoadBalancer),
			ds("nginx-ds", "default", appsv1.RollingUpdateDaemonSetStrategyType, false),
			ss("nginx-ss", "default", appsv1.RollingUpdateStatefulSetStrategyType, false),
			ing("test-ingress", "default", false),
			//crd("test-crd", "default", false, false),
			//crd("test-crd-foo", "default", true, false),
			//crdBeta("test-crd-beta", "default", false, false),
			//crdBeta("test-crd-beta-foo", "default", true, false),
		),
		HelmClient: h,
		Settings:   cli.New(),
	}
	c.AWSClients = &mockAWSClients{AWSSession: MockSession}
	if m != nil {
		c.LambdaResource = newLambdaResource(c.AWSClients.STSClient(nil, nil), m.ClusterID, m.KubeConfig, m.VPCConfiguration)
	}
	return c
}

func ObjBody(codec runtime.Codec, obj runtime.Object) io.ReadCloser {
	return ioutil.NopCloser(bytes.NewReader([]byte(runtime.EncodeOrDie(codec, obj))))
}

func testRESTMapper() meta.RESTMapper {
	groupResources := testDynamicResources()
	mapper := restmapper.NewDiscoveryRESTMapper(groupResources)
	// for backwards compatibility with existing tests, allow rest mappings from the scheme to show up
	// TODO: make this opt-in?
	mapper = meta.FirstHitRESTMapper{
		MultiRESTMapper: meta.MultiRESTMapper{
			mapper,
			testrestmapper.TestOnlyStaticRESTMapper(runtime.NewScheme()),
		},
	}

	fakeDs := &fakeCachedDiscoveryClient{}
	expander := restmapper.NewShortcutExpander(mapper, fakeDs)
	return expander
}

func testDynamicResources() []*restmapper.APIGroupResources {
	return []*restmapper.APIGroupResources{
		{
			Group: metav1.APIGroup{
				Versions: []metav1.GroupVersionForDiscovery{
					{Version: "v1"},
				},
				PreferredVersion: metav1.GroupVersionForDiscovery{Version: "v1"},
			},
			VersionedResources: map[string][]metav1.APIResource{
				"v1": {
					{Name: "pods", Namespaced: true, Kind: "Pod"},
					{Name: "services", Namespaced: true, Kind: "Service"},
					{Name: "replicationcontrollers", Namespaced: true, Kind: "ReplicationController"},
					{Name: "componentstatuses", Namespaced: false, Kind: "ComponentStatus"},
					{Name: "nodes", Namespaced: false, Kind: "Node"},
					{Name: "secrets", Namespaced: true, Kind: "Secret"},
					{Name: "configmaps", Namespaced: true, Kind: "ConfigMap"},
					{Name: "namespacedtype", Namespaced: true, Kind: "NamespacedType"},
					{Name: "namespaces", Namespaced: false, Kind: "Namespace"},
					{Name: "resourcequotas", Namespaced: true, Kind: "ResourceQuota"},
				},
			},
		},
		{
			Group: metav1.APIGroup{
				Name: "extensions",
				Versions: []metav1.GroupVersionForDiscovery{
					{Version: "v1beta1"},
				},
				PreferredVersion: metav1.GroupVersionForDiscovery{Version: "v1beta1"},
			},
			VersionedResources: map[string][]metav1.APIResource{
				"v1beta1": {
					{Name: "deployments", Namespaced: true, Kind: "Deployment"},
					{Name: "replicasets", Namespaced: true, Kind: "ReplicaSet"},
				},
			},
		},
		{
			Group: metav1.APIGroup{
				Name: "apps",
				Versions: []metav1.GroupVersionForDiscovery{
					{Version: "v1beta1"},
					{Version: "v1beta2"},
					{Version: "v1"},
				},
				PreferredVersion: metav1.GroupVersionForDiscovery{Version: "v1"},
			},
			VersionedResources: map[string][]metav1.APIResource{
				"v1beta1": {
					{Name: "deployments", Namespaced: true, Kind: "Deployment"},
					{Name: "replicasets", Namespaced: true, Kind: "ReplicaSet"},
				},
				"v1beta2": {
					{Name: "deployments", Namespaced: true, Kind: "Deployment"},
				},
				"v1": {
					{Name: "deployments", Namespaced: true, Kind: "Deployment"},
					{Name: "replicasets", Namespaced: true, Kind: "ReplicaSet"},
					{Name: "statefulsets", Namespaced: true, Kind: "StatefulSet"},
					{Name: "daemonsets", Namespaced: true, Kind: "DaemonSet"},
				},
			},
		},
		{
			Group: metav1.APIGroup{
				Name: "networking.k8s.io",
				Versions: []metav1.GroupVersionForDiscovery{
					{Version: "v1beta1"},
					{Version: "v0"},
				},
				PreferredVersion: metav1.GroupVersionForDiscovery{Version: "v1beta1"},
			},
			VersionedResources: map[string][]metav1.APIResource{
				"v1beta1": {
					{Name: "ingress", Namespaced: true, Kind: "Ingress"},
				},
			},
		},
		{
			Group: metav1.APIGroup{
				Name: "apiextensions.k8s.io",
				Versions: []metav1.GroupVersionForDiscovery{
					{Version: "v1beta1"},
					{Version: "v1"},
				},
				PreferredVersion: metav1.GroupVersionForDiscovery{Version: "v1"},
			},
			VersionedResources: map[string][]metav1.APIResource{
				"v1beta1": {
					{Name: "customresourcedefinition", Namespaced: true, Kind: "CustomResourceDefinition"},
				},
				"v1": {
					{Name: "customresourcedefinition", Namespaced: true, Kind: "CustomResourceDefinition"},
				},
			},
		},
	}
}

func ActionConfigFixture(t *testing.T) *action.Configuration {
	t.Helper()
	var verbose = aws.Bool(false)
	return &action.Configuration{
		Releases:     storage.Init(driver.NewMemory()),
		KubeClient:   &kubefake.FailingKubeClient{PrintingKubeClient: kubefake.PrintingKubeClient{Out: ioutil.Discard}},
		Capabilities: chartutil.DefaultCapabilities,
		Log: func(format string, v ...interface{}) {
			t.Helper()
			if *verbose {
				t.Logf(format, v...)
			}
		},
	}
}

func awsRequest(op *request.Operation, input, output interface{}) *request.Request {
	c := MockSession.ClientConfig("Mock", aws.NewConfig().WithRegion("us-east-2"))
	metaR := metadata.ClientInfo{
		ServiceName:   "Mock",
		SigningRegion: c.SigningRegion,
		Endpoint:      c.Endpoint,
		APIVersion:    "2015-12-08",
		JSONVersion:   "1.1",
		TargetPrefix:  "MockServer",
	}
	return request.New(*c.Config, metaR, c.Handlers, nil, op, input, output)
}

func makeMeSomeReleases(store *storage.Storage, t *testing.T) {
	t.Helper()
	one := namedRelease("one", release.StatusDeployed)
	one.Namespace = "default"
	one.Version = 1
	one.Manifest = TestManifest
	two := namedRelease("two", release.StatusFailed)
	two.Namespace = "default"
	two.Version = 2
	two.Manifest = TestManifest
	three := namedRelease("three", release.StatusDeployed)
	three.Namespace = "default"
	three.Version = 3
	three.Manifest = TestPendingManifest
	four := namedRelease("four", "unknown)")
	four.Namespace = "default"
	four.Version = 3
	four.Manifest = TestManifest
	five := namedRelease("five", release.StatusPendingUpgrade)
	five.Namespace = "default"
	five.Version = 3
	five.Manifest = TestManifest
	update := namedRelease("update", release.StatusDeployed)
	update.Namespace = "default"
	update.Info.Description = "eyJDbHVzdGVySUQiOiJla3MiLCJSZWdpb24iOiJldS13ZXN0LTEiLCJOYW1lIjoib25lIiwiTmFtZXNwYWNlIjoiZGVmYXVsdCJ9"
	update.Version = 1
	update.Manifest = TestManifest

	for _, rel := range []*release.Release{one, two, three, four, five} {
		if err := store.Create(rel); err != nil {
			t.Fatal(err)
		}
	}
}

func namedRelease(name string, status release.Status) *release.Release {
	now := htime.Now()
	return &release.Release{
		Name: name,
		Info: &release.Info{
			FirstDeployed: now,
			LastDeployed:  now,
			Status:        status,
			Description:   "umock-id",
		},
		Chart:   buildChart(),
		Version: 1,
	}
}

func buildChart(opts ...chartOption) *chart.Chart {
	c := &chartOptions{
		Chart: &chart.Chart{
			// TODO: This should be more complete.
			Metadata: &chart.Metadata{
				APIVersion: "v1",
				Name:       "hello",
				Version:    "0.1.0",
			},
			// This adds a basic template and hooks.
			Templates: []*chart.File{
				{Name: "templates/temp", Data: []byte(TestManifest)},
			},
		},
	}

	for _, opt := range opts {
		opt(c)
	}

	return c.Chart
}

func svc(name string, namespace string, sType v1.ServiceType) *v1.Service {
	var ingress []v1.LoadBalancerIngress
	if sType == v1.ServiceTypeLoadBalancer {
		ingress = []v1.LoadBalancerIngress{{Hostname: "elb.test.com"}}
	}
	return &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: corev1.ServiceSpec{
			Type:      sType,
			ClusterIP: "127.0.0.1",
		},
		Status: corev1.ServiceStatus{
			LoadBalancer: v1.LoadBalancerStatus{
				Ingress: ingress,
			},
		},
	}
}

func dep(name string, namespace string, pending bool) *appsv1.Deployment {
	count := int32(1)
	rcount := int32(1)
	if pending {
		rcount = int32(0)
	}
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: aws.Int32(count),
		},
		Status: appsv1.DeploymentStatus{
			ReadyReplicas: rcount,
		},
	}
}

func ds(name string, namespace string, dtype appsv1.DaemonSetUpdateStrategyType, pending bool) *appsv1.DaemonSet {
	count := int32(1)
	rcount := int32(1)
	dcount := int32(1)
	ucount := int32(1)
	if pending {
		dcount = int32(1)
		rcount = int32(0)
		count = int32(1)
		ucount = int32(0)
	}
	updateS := appsv1.DaemonSetUpdateStrategy{Type: dtype}
	if dtype == appsv1.RollingUpdateDaemonSetStrategyType {
		maxU := intstr.FromInt(0)
		updateS = appsv1.DaemonSetUpdateStrategy{Type: dtype,
			RollingUpdate: &appsv1.RollingUpdateDaemonSet{
				MaxUnavailable: &maxU,
			}}
	}
	return &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: appsv1.DaemonSetSpec{
			UpdateStrategy: updateS,
		},
		Status: appsv1.DaemonSetStatus{
			DesiredNumberScheduled: dcount,
			NumberReady:            rcount,
			NumberAvailable:        count,
			UpdatedNumberScheduled: ucount,
		},
	}
}

func ss(name string, namespace string, dtype appsv1.StatefulSetUpdateStrategyType, pending bool) *appsv1.StatefulSet {
	count := int32(2)
	rcount := int32(2)
	ucount := int32(1)
	if pending {
		rcount = int32(0)
		ucount = int32(1)
	}
	updateS := appsv1.StatefulSetUpdateStrategy{Type: dtype}
	if dtype == appsv1.RollingUpdateStatefulSetStrategyType {
		updateS = appsv1.StatefulSetUpdateStrategy{Type: dtype,
			RollingUpdate: &appsv1.RollingUpdateStatefulSetStrategy{
				Partition: aws.Int32(1),
			}}
	}
	return &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas:       aws.Int32(count),
			UpdateStrategy: updateS,
		},
		Status: appsv1.StatefulSetStatus{
			ReadyReplicas:   rcount,
			UpdatedReplicas: ucount,
		},
	}
}

func ing(name string, namespace string, pending bool) *v1beta1.Ingress {
	var ingress []v1.LoadBalancerIngress
	if !pending {
		ingress = []v1.LoadBalancerIngress{{Hostname: "ingress.test.com"}}
	}
	return &v1beta1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Status: v1beta1.IngressStatus{
			LoadBalancer: v1.LoadBalancerStatus{
				Ingress: ingress,
			},
		},
	}
}

func ingN(name string, namespace string, pending bool) *networkingv1beta1.Ingress {
	var ingress []v1.LoadBalancerIngress
	if !pending {
		ingress = []v1.LoadBalancerIngress{{Hostname: "ingressN.test.com"}}
	}
	return &networkingv1beta1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Status: networkingv1beta1.IngressStatus{
			LoadBalancer: v1.LoadBalancerStatus{
				Ingress: ingress,
			},
		},
	}
}

func ns(name string) *v1.Namespace {
	return &v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
}

func vol(name string, namespace string, pending bool) *corev1.PersistentVolumeClaim {
	p := corev1.ClaimBound
	if pending {
		p = corev1.ClaimPending
	}
	return &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Status: corev1.PersistentVolumeClaimStatus{
			Phase: p,
		},
	}
}

func crd(name string, namespace string, namesAccepted bool, pending bool) *apiextv1.CustomResourceDefinition {
	s := apiextv1.ConditionTrue
	if pending {
		s = apiextv1.ConditionFalse
	}
	c := []apiextv1.CustomResourceDefinitionCondition{{
		Type:   apiextv1.Established,
		Status: s,
	},
	}
	switch {
	case namesAccepted && !pending:
		c = []apiextv1.CustomResourceDefinitionCondition{{
			Type:   apiextv1.NamesAccepted,
			Status: apiextv1.ConditionFalse,
		},
		}
	case namesAccepted && pending:
		c = []apiextv1.CustomResourceDefinitionCondition{{
			Type:   apiextv1.NamesAccepted,
			Status: apiextv1.ConditionTrue,
		},
		}
	}

	return &apiextv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Status: apiextv1.CustomResourceDefinitionStatus{Conditions: c},
	}
}

func crdBeta(name string, namespace string, namesAccepted bool, pending bool) *apiextv1beta1.CustomResourceDefinition {
	s := apiextv1beta1.ConditionTrue
	if pending {
		s = apiextv1beta1.ConditionFalse
	}
	c := []apiextv1beta1.CustomResourceDefinitionCondition{{
		Type:   apiextv1beta1.Established,
		Status: s,
	},
	}
	switch {
	case namesAccepted && !pending:
		c = []apiextv1beta1.CustomResourceDefinitionCondition{{
			Type:   apiextv1beta1.NamesAccepted,
			Status: apiextv1beta1.ConditionFalse,
		},
		}
	case namesAccepted && pending:
		c = []apiextv1beta1.CustomResourceDefinitionCondition{{
			Type:   apiextv1beta1.NamesAccepted,
			Status: apiextv1beta1.ConditionTrue,
		},
		}
	}

	return &apiextv1beta1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Status: apiextv1beta1.CustomResourceDefinitionStatus{Conditions: c},
	}
}
