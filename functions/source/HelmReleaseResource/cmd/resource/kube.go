package resource

import (
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"reflect"
	"regexp"

	"helm.sh/helm/v3/pkg/kube"
	appsv1 "k8s.io/api/apps/v1"
	appsv1beta1 "k8s.io/api/apps/v1beta1"
	appsv1beta2 "k8s.io/api/apps/v1beta2"
	corev1 "k8s.io/api/core/v1"
	extensionsv1beta1 "k8s.io/api/extensions/v1beta1"
	networkingv1beta1 "k8s.io/api/networking/v1beta1"
	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/cli-runtime/pkg/resource"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"
)

const (
	KubeConfigLocalPath = "/tmp/kubeConfig"
	TempManifest        = "/tmp/manifest.yaml"
	chunkSize           = 500
	ResourcesOutputSize = 12288 // Set 12 KB as resources output limit
)

var (
	ResourcesOutputIgnoredTypes = []string{"*v1.ConfigMap", "*v1.Secret"}
	ResourcesOutputIncludedSpec = []string{"*v1.Service"}
)

type ReleaseData struct {
	Name, Chart, Namespace, Manifest string `json:",omitempty"`
}

// createKubeConfig create kubeconfig from ClusterID or Secret manager.
func createKubeConfig(esvc EKSAPI, ssvc STSAPI, secsvc SecretsManagerAPI, cluster *string, kubeconfig *string, customKubeconfig []byte) error {
	switch {
	case cluster != nil && kubeconfig != nil:
		return errors.New("both ClusterID or KubeConfig can not be specified")
	case cluster != nil:
		defaultConfig := api.NewConfig()
		c, err := getClusterDetails(esvc, *cluster)
		if err != nil {
			return genericError("Getting Cluster details", err)
		}
		defaultConfig.Clusters[*cluster] = &api.Cluster{
			Server:                   c.endpoint,
			CertificateAuthorityData: []byte(c.CAData),
		}
		token, err := generateKubeToken(ssvc, cluster)
		if err != nil {
			return err
		}
		defaultConfig.AuthInfos["aws"] = &api.AuthInfo{
			Token: *token,
		}
		defaultConfig.Contexts["aws"] = &api.Context{
			Cluster:  *cluster,
			AuthInfo: "aws",
		}
		defaultConfig.CurrentContext = "aws"
		log.Printf("Writing kubeconfig file to %s", KubeConfigLocalPath)

		err = clientcmd.WriteToFile(*defaultConfig, KubeConfigLocalPath)
		if err != nil {
			return genericError("Write file: ", err)
		}
		return nil
	case kubeconfig != nil:
		s, err := getSecretsManager(secsvc, kubeconfig)
		if err != nil {
			return err
		}
		log.Printf("Writing kubeconfig file to %s", KubeConfigLocalPath)
		err = ioutil.WriteFile(KubeConfigLocalPath, s, 0600)
		if err != nil {
			return genericError("Write file: ", err)
		}
		return nil
	case customKubeconfig != nil:
		log.Printf("Writing kubeconfig file to %s", KubeConfigLocalPath)
		err := ioutil.WriteFile(KubeConfigLocalPath, customKubeconfig, 0600)
		if err != nil {
			return genericError("Write file: ", err)
		}
		return nil
	default:
		return errors.New("either ClusterID or KubeConfig must be specified")
	}
}

// createNamespace create NS if not exists
func (c *Clients) createNamespace(namespace string) error {
	nsSpec := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
	_, err := c.ClientSet.CoreV1().Namespaces().Create(context.Background(), nsSpec, metav1.CreateOptions{})
	switch err {
	case nil:
		return nil
	default:
		switch kerrors.IsAlreadyExists(err) {
		case true:
			log.Printf("Namespace : %s. Already exists. Continue to install...", namespace)
			return nil
		default:
			return genericError("Create NS", err)
		}
	}
}

// CheckPendingResources checks pending resources in for the specific release.
func (c *Clients) CheckPendingResources(r *ReleaseData) (bool, error) {
	log.Printf("Checking pending resources in %s", r.Name)
	var err error
	var errCount int
	var pArray []bool
	if r.Manifest == "" {
		return true, errors.New("Manifest not provided in the request")
	}
	infos, err := c.getManifestDetails(r)
	if err != nil {
		// Retry if resources not found
		// todo: Need to have retry count
		re := regexp.MustCompile("not found")
		if re.MatchString(err.Error()) {
			log.Println(err.Error())
			return true, nil
		}
		return true, err
	}
	for _, info := range infos {
		if errCount >= retryCount*2 {
			return true, fmt.Errorf("couldn't get the resources")
		}
		switch value := kube.AsVersioned(info).(type) {
		case *appsv1.Deployment, *appsv1beta1.Deployment, *appsv1beta2.Deployment, *extensionsv1beta1.Deployment:
			currentDeployment, err := c.ClientSet.AppsV1().Deployments(info.Namespace).Get(context.Background(), info.Name, metav1.GetOptions{})
			if err != nil {
				errCount++
				log.Printf("Warning: Got error getting deployment %s", err.Error())
				continue
			}
			// If paused deployment will never be ready
			if currentDeployment.Spec.Paused {
				continue
			}
			if !deploymentReady(currentDeployment) {
				pArray = append(pArray, false)
			}
		case *corev1.PersistentVolumeClaim:
			if !volumeReady(value) {
				pArray = append(pArray, false)
			}
		case *corev1.Service:
			if !serviceReady(value) {
				pArray = append(pArray, false)
			}
		case *extensionsv1beta1.DaemonSet, *appsv1.DaemonSet, *appsv1beta2.DaemonSet:
			ds, err := c.ClientSet.AppsV1().DaemonSets(info.Namespace).Get(context.Background(), info.Name, metav1.GetOptions{})

			if err != nil {
				log.Printf("Warning: Got error getting daemonset %s", err.Error())
				errCount++
				continue
			}
			if !daemonSetReady(ds) {
				pArray = append(pArray, false)
			}
		case *appsv1.StatefulSet, *appsv1beta1.StatefulSet, *appsv1beta2.StatefulSet:
			sts, err := c.ClientSet.AppsV1().StatefulSets(info.Namespace).Get(context.Background(), info.Name, metav1.GetOptions{})
			if err != nil {
				log.Printf("Warning: Got error getting statefulset %s", err.Error())
				errCount++
				continue
			}
			if !statefulSetReady(sts) {
				pArray = append(pArray, false)
			}
		case *extensionsv1beta1.Ingress:
			if !ingressReady(value) {
				pArray = append(pArray, false)
			}
		case *networkingv1beta1.Ingress:
			if !ingressNReady(value) {
				pArray = append(pArray, false)
			}
		case *apiextv1beta1.CustomResourceDefinition:
			if err := info.Get(); err != nil {
				return false, err
			}
			crd := &apiextv1beta1.CustomResourceDefinition{}
			if err := scheme.Scheme.Convert(info.Object, crd, nil); err != nil {
				log.Printf("Warning: Got error getting CRD %s", err.Error())
				errCount++
				continue
			}
			if !crdBetaReady(crd) {
				pArray = append(pArray, false)
			}
		case *apiextv1.CustomResourceDefinition:
			if err := info.Get(); err != nil {
				return false, err
			}
			crd := &apiextv1.CustomResourceDefinition{}
			if err := scheme.Scheme.Convert(info.Object, crd, nil); err != nil {
				log.Printf("Warning: Got error getting CRD %s", err.Error())
				errCount++
				continue
			}
			if !crdReady(crd) {
				pArray = append(pArray, false)
			}
		}
	}
	if len(pArray) > 0 || errCount != 0 {
		return true, err
	}
	return false, err
}

// GetKubeResources get resources for the specific release.
func (c *Clients) GetKubeResources(r *ReleaseData) (map[string]interface{}, error) {
	log.Printf("Getting resources for %s", r.Name)
	if r.Manifest == "" {
		return nil, errors.New("manifest not provided in the request")
	}
	resources := map[string]interface{}{}
	infos, err := c.getManifestDetails(r)
	if err != nil {
		return nil, err
	}
	namespace := "default"
	for _, info := range infos {
		var spec interface{}
		kind := info.Object.GetObjectKind().GroupVersionKind().GroupKind().Kind
		v := kube.AsVersioned(info)
		if checkSize(resources, ResourcesOutputSize) {
			break
		}

		if stringInSlice(reflect.TypeOf(v).String(), ResourcesOutputIgnoredTypes) {
			continue
		}
		inner := make(map[string]interface{})
		name, ok := ScanFromStruct(v, "ObjectMeta.Name")
		if !ok {
			continue
		}
		ns, ok := ScanFromStruct(v, "ObjectMeta.Namespace")
		if ok {
			namespace = fmt.Sprint(ns)
		}
		if stringInSlice(reflect.TypeOf(v).String(), ResourcesOutputIncludedSpec) {
			spec, ok = ScanFromStruct(v, "Spec")
			if ok {
				spec = structToMap(spec)
			}
		}
		status, ok := ScanFromStruct(v, "Status")
		if ok {
			status = structToMap(status)
		}
		inner = map[string]interface{}{
			fmt.Sprint(name): map[string]interface{}{
				"Namespace": namespace,
				"Spec":      spec,
				"Status":    status,
			},
		}
		if IsZero(resources[kind]) {
			resources[kind] = map[string]interface{}{}
		}
		temp := resources[kind].(map[string]interface{})
		resources[kind] = mergeMaps(temp, inner)
	}
	return resources, nil
}

func (c *Clients) getManifestDetails(r *ReleaseData) ([]*resource.Info, error) {
	log.Printf("Getting resources for %s's manifest", r.Name)

	err := ioutil.WriteFile(TempManifest, []byte(r.Manifest), 0600)
	if err != nil {
		return nil, genericError("Write manifest file: ", err)
	}

	f := &resource.FilenameOptions{
		Filenames: []string{TempManifest},
	}

	res := c.ResourceBuilder().
		Unstructured().
		NamespaceParam(r.Namespace).DefaultNamespace().AllNamespaces(false).
		FilenameParam(false, f).
		RequestChunksOf(chunkSize).
		ContinueOnError().
		Latest().
		Flatten().
		TransformRequests().
		Do()

	infos, err := res.Infos()
	if err != nil {
		return nil, err
	}
	return infos, nil
}

func ingressReady(i *extensionsv1beta1.Ingress) bool {
	if IsZero(i.Status.LoadBalancer) {
		msg := fmt.Sprintf("Ingress does not have address: %s/%s", i.GetNamespace(), i.GetName())
		log.Printf(msg)
		pushLastKnownError(msg)
		return false
	}
	popLastKnownError(i.GetName())
	return true
}

func ingressNReady(i *networkingv1beta1.Ingress) bool {
	if IsZero(i.Status.LoadBalancer) {
		msg := fmt.Sprintf("Ingress does not have address: %s/%s", i.GetNamespace(), i.GetName())
		log.Printf(msg)
		pushLastKnownError(msg)
		return false
	}
	popLastKnownError(i.GetName())
	return true
}

func volumeReady(v *corev1.PersistentVolumeClaim) bool {
	if v.Status.Phase != corev1.ClaimBound {
		msg := fmt.Sprintf("PersistentVolumeClaim is not bound: %s/%s", v.GetNamespace(), v.GetName())
		log.Printf(msg)
		pushLastKnownError(msg)
		return false
	}
	popLastKnownError(v.GetName())
	return true
}

func serviceReady(s *corev1.Service) bool {
	// ExternalName Services are external to cluster so helm shouldn't be checking to see if they're 'ready' (i.e. have an IP Set)
	if s.Spec.Type == corev1.ServiceTypeExternalName {
		return true
	}

	// Make sure the service is not explicitly set to "None" before checking the IP
	if s.Spec.ClusterIP != corev1.ClusterIPNone && s.Spec.ClusterIP == "" {
		msg := fmt.Sprintf("Service does not have cluster IP address: %s/%s", s.GetNamespace(), s.GetName())
		log.Printf(msg)
		pushLastKnownError(msg)
		return false
	}

	// This checks if the service has a LoadBalancer and that balancer has an Ingress defined
	if s.Spec.Type == corev1.ServiceTypeLoadBalancer {
		// do not wait when at least 1 external IP is set
		if len(s.Spec.ExternalIPs) > 0 {
			log.Printf("Service %s/%s has external IP addresses (%v), marking as ready", s.GetNamespace(), s.GetName(), s.Spec.ExternalIPs)
			popLastKnownError(s.GetName())
			return true
		}

		if s.Status.LoadBalancer.Ingress == nil {
			msg := fmt.Sprintf("Service does not have load balancer ingress IP address: %s/%s", s.GetNamespace(), s.GetName())
			log.Printf(msg)
			pushLastKnownError(msg)
			return false
		}
	}
	popLastKnownError(s.GetName())
	return true
}

func deploymentReady(dep *appsv1.Deployment) bool {
	if !(dep.Status.ReadyReplicas >= *dep.Spec.Replicas) {
		msg := fmt.Sprintf("Deployment is not ready: %s/%s. %d out of %d expected pods are ready", dep.Namespace, dep.Name, dep.Status.ReadyReplicas, *dep.Spec.Replicas)
		log.Printf(msg)
		pushLastKnownError(msg)
		return false
	}
	popLastKnownError(dep.GetName())
	return true
}

func daemonSetReady(ds *appsv1.DaemonSet) bool {
	// If the update strategy is not a rolling update, there will be nothing to wait for
	if ds.Spec.UpdateStrategy.Type != appsv1.RollingUpdateDaemonSetStrategyType {
		return true
	}

	// Make sure all the updated pods have been scheduled
	if ds.Status.UpdatedNumberScheduled != ds.Status.DesiredNumberScheduled {
		msg := fmt.Sprintf("DaemonSet is not ready: %s/%s. %d out of %d expected pods have been scheduled", ds.Namespace, ds.Name, ds.Status.UpdatedNumberScheduled, ds.Status.DesiredNumberScheduled)
		log.Printf(msg)
		pushLastKnownError(msg)
		return false
	}
	maxUnavailable, err := intstr.GetValueFromIntOrPercent(ds.Spec.UpdateStrategy.RollingUpdate.MaxUnavailable, int(ds.Status.DesiredNumberScheduled), true)
	if err != nil {
		maxUnavailable = int(ds.Status.DesiredNumberScheduled)
	}

	expectedReady := int(ds.Status.DesiredNumberScheduled) - maxUnavailable
	if !(int(ds.Status.NumberReady) >= expectedReady) {
		msg := fmt.Sprintf("DaemonSet is not ready: %s/%s. %d out of %d expected pods are ready", ds.Namespace, ds.Name, ds.Status.NumberReady, expectedReady)
		log.Printf(msg)
		pushLastKnownError(msg)
		return false
	}
	popLastKnownError(ds.GetName())
	return true
}

func statefulSetReady(sts *appsv1.StatefulSet) bool {
	// If the update strategy is not a rolling update, there will be nothing to wait for
	if sts.Spec.UpdateStrategy.Type != appsv1.RollingUpdateStatefulSetStrategyType {
		return true
	}

	// Dereference all the pointers because StatefulSets like them
	var partition int
	// 1 is the default for replicas if not set
	var replicas = 1
	// For some reason, even if the update strategy is a rolling update, the
	// actual rollingUpdate field can be nil. If it is, we can safely assume
	// there is no partition value
	if sts.Spec.UpdateStrategy.RollingUpdate != nil && sts.Spec.UpdateStrategy.RollingUpdate.Partition != nil {
		partition = int(*sts.Spec.UpdateStrategy.RollingUpdate.Partition)
	}
	if sts.Spec.Replicas != nil {
		replicas = int(*sts.Spec.Replicas)
	}

	// Because an update strategy can use partitioning, we need to calculate the
	// number of updated replicas we should have. For example, if the replicas
	// is set to 3 and the partition is 2, we'd expect only one pod to be
	// updated
	expectedReplicas := replicas - partition

	// Make sure all the updated pods have been scheduled
	if int(sts.Status.UpdatedReplicas) != expectedReplicas {
		msg := fmt.Sprintf("StatefulSet is not ready: %s/%s. %d out of %d expected pods have been scheduled", sts.Namespace, sts.Name, sts.Status.UpdatedReplicas, expectedReplicas)
		log.Printf(msg)
		pushLastKnownError(msg)
		return false
	}

	if int(sts.Status.ReadyReplicas) != replicas {
		msg := fmt.Sprintf("StatefulSet is not ready: %s/%s. %d out of %d expected pods are ready", sts.Namespace, sts.Name, sts.Status.ReadyReplicas, replicas)
		log.Printf(msg)
		pushLastKnownError(msg)
		return false
	}
	popLastKnownError(sts.GetName())
	return true
}

func crdBetaReady(crd *apiextv1beta1.CustomResourceDefinition) bool {
	for _, cond := range crd.Status.Conditions {
		switch cond.Type {
		case apiextv1beta1.Established:
			if cond.Status == apiextv1beta1.ConditionTrue {
				popLastKnownError(crd.Name)
				return true
			}
		case apiextv1beta1.NamesAccepted:
			if cond.Status == apiextv1beta1.ConditionFalse {
				// This indicates a naming conflict, but it's probably not the
				// job of this function to fail because of that. Instead,
				// we treat it as a success, since the process should be able to
				// continue.
				popLastKnownError(crd.Name)
				return true
			}
		}
	}
	msg := fmt.Sprintf("CRD is not ready %s/%s.", crd.Namespace, crd.Name)
	log.Printf(msg)
	pushLastKnownError(msg)
	return false
}

func crdReady(crd *apiextv1.CustomResourceDefinition) bool {
	for _, cond := range crd.Status.Conditions {
		switch cond.Type {
		case apiextv1.Established:
			if cond.Status == apiextv1.ConditionTrue {
				popLastKnownError(crd.Name)
				return true
			}
		case apiextv1.NamesAccepted:
			if cond.Status == apiextv1.ConditionFalse {
				// This indicates a naming conflict, but it's probably not the
				// job of this function to fail because of that. Instead,
				// we treat it as a success, since the process should be able to
				// continue.
				popLastKnownError(crd.Name)
				return true
			}
		}
	}
	msg := fmt.Sprintf("CRD is not ready %s/%s.", crd.Namespace, crd.Name)
	log.Printf(msg)
	pushLastKnownError(msg)
	return false
}
