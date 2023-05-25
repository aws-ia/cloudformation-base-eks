package resource

import (
	"bytes"
	"crypto/md5"
	"encoding/base64"
	"encoding/gob"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"regexp"
	"runtime/debug"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/session"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/registry"
	"helm.sh/helm/v3/pkg/strvals"
	"k8s.io/cli-runtime/pkg/resource"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/yaml"
)

const (
	valuesYamlFile = "/tmp/values.yaml"
	defaultTimeOut = 60
)

// ID struct for CFN physical resource
type ID struct {
	ClusterID        *string           `json:",omitempty"`
	KubeConfig       *string           `json:",omitempty"`
	Region           *string           `json:",omitempty"`
	Name             *string           `json:",omitempty"`
	Namespace        *string           `json:",omitempty"`
	VPCConfiguration *VPCConfiguration `json:",omitempty"`
}

type ClientsInterface interface{}

// Clients for helm, kube, aws and helm settings
type Clients struct {
	AWSClients      AWSClientsIface
	HelmClient      *action.Configuration `json:",omitempty"`
	ClientSet       kubernetes.Interface  `json:",omitempty"`
	Settings        *cli.EnvSettings      `json:",omitempty"`
	ResourceBuilder func() *resource.Builder
	LambdaResource  *lambdaResource
}

// Config for processed inputs
type Config struct {
	Name, Namespace *string `json:",omitempty"`
}

// Chart for chart data
type Chart struct {
	Chart, ChartName, ChartPath, ChartType, ChartRepo, ChartVersion, ChartRepoURL, ChartUsername, ChartPassword *string `json:",omitempty"`
	ChartSkipTLSVerify, ChartLocalCA                                                                            *bool   `json:",omitempty"`
}

//Inputs for Config and Values for helm
type Inputs struct {
	Config       *Config                `json:",omitempty"`
	ChartDetails *Chart                 `json:",omitempty"`
	ValueOpts    map[string]interface{} `json:",omitempty"`
}

// NewClients is for generate clients for helm, kube and AWS
var NewClients = func(cluster *string, kubeconfig *string, namespace *string, ses *session.Session, role *string, customKubeconfig []byte, vpcConfig *VPCConfiguration) (*Clients, error) {
	var err error
	c := &Clients{}
	if ses == nil {
		ses, err = session.NewSession()
		if err != nil {
			return nil, err
		}
	}
	c.AWSClients = &AWSClients{AWSSession: ses}
	if err := createKubeConfig(c.AWSClients.EKSClient(nil, nil), c.AWSClients.STSClient(nil, role), c.AWSClients.SecretsManagerClient(nil, nil), cluster, kubeconfig, customKubeconfig); err != nil {
		return nil, err
	}
	if namespace == nil {
		namespace = aws.String("default")
	}
	os.Setenv("HELM_NAMESPACE", aws.StringValue(namespace))
	c.Settings = cli.New()
	c.HelmClient, err = helmClientInvoke(namespace, c.Settings.RESTClientGetter())
	if err != nil {
		return nil, err
	}
	c.ClientSet, err = c.HelmClient.KubernetesClientSet()
	if err != nil {
		return nil, err
	}

	c.ResourceBuilder = func() *resource.Builder {
		return resource.NewBuilder(c.Settings.RESTClientGetter())
	}
	c.LambdaResource = newLambdaResource(c.AWSClients.STSClient(nil, nil), cluster, kubeconfig, vpcConfig)
	return c, nil
}

//Process the values in the input
func (c *Clients) processValues(m *Model) (map[string]interface{}, error) {
	values := map[string]interface{}{}
	valueYaml := map[string]interface{}{}
	currentMap := map[string]interface{}{}
	if m.ValueYaml != nil {
		err := yaml.Unmarshal([]byte(*m.ValueYaml), &valueYaml)
		if err != nil {
			return nil, err
		}
	}
	if m.Values != nil {
		for k, v := range m.Values {
			if err := strvals.ParseInto(fmt.Sprintf("%s=%s", k, v), values); err != nil {
				return nil, genericError("Processing values", err)
			}
		}
	}
	base := mergeMaps(valueYaml, values)
	if m.ValueOverrideURL != nil {
		u, err := url.Parse(*m.ValueOverrideURL)
		if err != nil {
			return nil, genericError("Process ValueOverrideURL ", err)
		}
		bucket := u.Host
		key := strings.TrimLeft(u.Path, "/")
		region, err := getBucketRegion(c.AWSClients.S3Client(nil, nil), bucket)
		if err != nil {
			return nil, err
		}
		err = downloadS3(c.AWSClients.S3Client(region, nil), bucket, key, valuesYamlFile)
		if err != nil {
			return nil, err
		}
		byteKey, err := ioutil.ReadFile(valuesYamlFile)
		if err != nil {
			return nil, genericError("Reading custom yaml", err)
		}
		if err := yaml.Unmarshal(byteKey, &currentMap); err != nil {
			return nil, genericError("Parsing yaml", err)
		}
	}
	return mergeMaps(base, currentMap), nil
}

// getChartDetails parse chart
func (c *Clients) getChartDetails(m *Model) (*Chart, error) {
	cd := &Chart{}
	// Parse chart
	switch m.Chart {
	case nil:
		return nil, errors.New("chart is required")
	default:
		// Check if chart is remote url
		u, err := url.Parse(*m.Chart)
		if err != nil {
			return nil, genericError("Process chart", err)
		}
		switch {
		case u.Host != "", strings.ToLower(u.Scheme) == "oci":
			cd.ChartType = aws.String("Local")
			cd.Chart = aws.String(chartLocalPath)
			cd.ChartPath = m.Chart
			var chart string
			sa := strings.Split(u.Path, "/")
			switch {
			case len(sa) > 1:
				chart = sa[len(sa)-1]
			default:
				chart = strings.TrimLeft(u.RequestURI(), "/")
			}
			re := regexp.MustCompile(`[A-Za-z]+`)
			cd.ChartName = aws.String(re.FindAllString(chart, 1)[0])
			if !IsZero(m.RepositoryOptions) {
				if !IsZero(m.RepositoryOptions.Username) && !IsZero(m.RepositoryOptions.Password) {
					log.Printf("Using basic authentication with username: %s for repository", *m.RepositoryOptions.Username)
					cd.ChartUsername = m.RepositoryOptions.Username
					cd.ChartPassword = m.RepositoryOptions.Password
				}
			}
		default:
			// Get repo name and chart
			sa := strings.Split(*m.Chart, "/")
			switch {
			case len(sa) > 1:
				cd.ChartRepo = aws.String(sa[0])
				cd.ChartName = aws.String(sa[1])
			default:
				cd.ChartRepo = aws.String("stable")
				cd.ChartName = m.Chart
			}
			// Set chart verify to default
			cd.ChartSkipTLSVerify = aws.Bool(false)
			cd.ChartLocalCA = aws.Bool(false)
			if !IsZero(m.RepositoryOptions) {
				if !IsZero(m.RepositoryOptions.Username) && !IsZero(m.RepositoryOptions.Password) {
					log.Printf("Using basic authentication with username: %s for repository", *m.RepositoryOptions.Username)
					cd.ChartUsername = m.RepositoryOptions.Username
					cd.ChartPassword = m.RepositoryOptions.Password
				}
				// IsZero on bool if false
				if !IsZero(m.RepositoryOptions.InsecureSkipTLSVerify) {
					cd.ChartSkipTLSVerify = m.RepositoryOptions.InsecureSkipTLSVerify
				}
				if !IsZero(m.RepositoryOptions.CAFile) {
					u, err := url.Parse(*m.RepositoryOptions.CAFile)
					if err != nil {
						return nil, genericError("Process url", err)
					}
					switch {
					case strings.ToLower(u.Scheme) == "s3":
						bucket := u.Host
						key := strings.TrimLeft(u.Path, "/")
						region, err := getBucketRegion(c.AWSClients.S3Client(nil, nil), bucket)
						if err != nil {
							return nil, err
						}
						err = downloadS3(c.AWSClients.S3Client(region, nil), bucket, key, caLocalPath)
						if err != nil {
							return nil, err
						}
						cd.ChartLocalCA = aws.Bool(true)
					default:
						log.Printf("Unsupported CAFile format: %s must be S3 path. Ignoring CAFile...", *m.RepositoryOptions.CAFile)
					}
				}
			}
			cd.ChartType = aws.String("Remote")
			cd.Chart = aws.String(fmt.Sprintf("%s/%s", *cd.ChartRepo, *cd.ChartName))
		}
	}
	if m.Version != nil {
		cd.ChartVersion = m.Version
	}
	switch m.Repository {
	case nil:
		cd.ChartRepoURL = aws.String(stableRepoURL)
	default:
		cd.ChartRepoURL = m.Repository
	}
	return cd, nil
}

func getReleaseName(name *string, chartname *string) *string {
	switch name {
	case nil:
		if chartname != nil {
			return aws.String(*chartname + "-" + fmt.Sprint(time.Now().Unix()))
		}
		return nil
	default:
		return name
	}
}

func getReleaseNameContext(context map[string]interface{}) *string {
	if context == nil {
		return nil
	}
	if context["Name"] == nil {
		return nil
	}
	return aws.String(fmt.Sprint(context["Name"]))
}

func getReleaseNameSpace(n *string) *string {
	switch n {
	case nil:
		return aws.String("default")
	default:
		return n
	}
}

//AWSError takes an AWS generated error and handles it
func AWSError(err error) error {
	if err == nil {
		return nil
	}
	if awsErr, ok := err.(awserr.Error); ok {
		// Get error details
		log.Printf("AWS Error: %s - %s %v - %v\n", awsErr.Code(), awsErr.Message(), awsErr.OrigErr(), awsErr.Error())

		// Prints out full error message, including original error if there was one.
		log.Printf("Error: %v", awsErr.Error())

		// Get original error
		if origErr := awsErr.OrigErr(); origErr != nil {
			// operate on original error.
		}
		return fmt.Errorf("AWS Error: %s - %s %v-%v", awsErr.Code(), awsErr.Message(), awsErr.OrigErr(), awsErr.Error())
	}
	return fmt.Errorf(err.Error())
}

//genericError takes  error, log it and return new err.
func genericError(source string, err error) error {
	log.Printf("Error: At %s - %s \n", source, err)
	return fmt.Errorf("Error: At %s - %s ", source, err)
}

// Merge values maps
func mergeMaps(a, b map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(a))
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		if v, ok := v.(map[string]interface{}); ok {
			if bv, ok := out[k]; ok {
				if bv, ok := bv.(map[string]interface{}); ok {
					out[k] = mergeMaps(bv, v)
					continue
				}
			}
		}
		out[k] = v
	}
	return out
}

// downloadHTTP downloads the file to specified path
func downloadHTTP(url, filepath string, username, password *string) error {
	log.Printf("Getting file from URL...")
	// Get the data
	//resp, err := http.Get(url)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return genericError("Generating request", err)
	}
	if !IsZero(username) && !IsZero(password) {
		req.SetBasicAuth(*username, *password)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return genericError("Downloading file", err)
	}

	if resp.StatusCode != 200 {
		return genericError("Downloading file", fmt.Errorf("got response %v", resp.StatusCode))
	}

	defer resp.Body.Close()

	// Create the file
	out, err := os.Create(filepath)
	if err != nil {
		return genericError("Creating file", err)
	}
	defer out.Close()

	// Write the body to file
	_, err = io.Copy(out, resp.Body)
	if err != nil {
		return genericError("Writing file", err)
	}
	log.Printf("Downloaded %s ", out.Name())
	return nil
}

//generateID is to generate physical id for CFN
func generateID(m *Model, name string, region string, namespace string) (*string, error) {
	i := &ID{}
	switch {
	case m.ClusterID != nil && m.KubeConfig != nil:
		return nil, fmt.Errorf("both ClusterID or KubeConfig can not be specified")
	case m.ClusterID != nil:
		i.ClusterID = m.ClusterID
	case m.KubeConfig != nil:
		i.KubeConfig = m.KubeConfig
	default:
		return nil, fmt.Errorf("either ClusterID or KubeConfig must be specified")
	}
	if name == "" || namespace == "" || region == "" {
		return nil, fmt.Errorf("incorrect values for variable name, namespace, region")
	}
	i.Name = aws.String(name)
	i.Namespace = aws.String(namespace)
	i.Region = aws.String(region)
	if !IsZero(m.VPCConfiguration) {
		i.VPCConfiguration = m.VPCConfiguration
	}
	out, err := json.Marshal(i)
	if err != nil {
		return nil, genericError("Json Marshal", err)
	}
	str := base64.RawURLEncoding.EncodeToString(out)
	return aws.String(str), nil
}

//DecodeID decodes the physical id provided by CFN
func DecodeID(id *string) (*ID, error) {
	i := &ID{}
	str, err := base64.RawURLEncoding.DecodeString(*id)
	if err != nil {
		return nil, genericError("Decode", err)
	}
	err = json.Unmarshal(str, i)
	if err != nil {
		return nil, genericError("Json Unmarshal", err)
	}
	return i, nil
}

// downloadChart downloads the chart
func (c *Clients) downloadChart(ur, f string, username, password *string) error {
	u, err := url.Parse(ur)
	if err != nil {
		return genericError("Process url", err)
	}
	switch {
	case strings.ToLower(u.Scheme) == "s3":
		bucket := u.Host
		key := strings.TrimLeft(u.Path, "/")
		region, err := getBucketRegion(c.AWSClients.S3Client(nil, nil), bucket)
		if err != nil {
			return err
		}
		err = downloadS3(c.AWSClients.S3Client(region, nil), bucket, key, f)
		if err != nil {
			return err
		}
	case strings.ToLower(u.Scheme) == "oci":
		var ecrRe = regexp.MustCompile(`(?smi)((\d+).dkr.ecr.(\w+-\w+-\d+).amazonaws.com)`)
		if ecrRe.MatchString(u.Host) {
			// Get region from the ECR endpoint
			hostParts := strings.Split(u.Host, ".")
			region := aws.String(hostParts[len(hostParts)-3])
			username, password, err = getECRLogin(c.AWSClients.ECRClient(region, nil))
			if err != nil {
				return err
			}
		}

		err = downloadOCI(u.Host, strings.TrimLeft(u.Path, "/"), aws.StringValue(username), aws.StringValue(password), f)
		if err != nil {
			return err
		}
	default:
		err = downloadHTTP(ur, f, username, password)
		if err != nil {
			return err
		}
	}
	return nil
}

// checkTimeOut is see if elapsed time crossed the timeout.
func checkTimeOut(startTime string, timeOut *int) bool {
	t, _ := time.Parse(time.RFC3339, startTime)
	var s time.Duration
	switch timeOut {
	case nil:
		s = defaultTimeOut * 60 * time.Second
	default:
		s = time.Duration(*timeOut) * 60 * time.Second
	}
	ts := time.Since(t).Seconds()
	log.Printf("Elapsed Time : %.0f sec, Timeout: %v sec", ts, s.Seconds())
	if ts >= s.Seconds() {
		return true
	}
	return false
}

func getStage(context map[string]interface{}) Stage {
	if context == nil {
		os.Setenv("StartTime", time.Now().Format(time.RFC3339))
		return InitStage
	}
	if context["Stage"] == nil {
		return InitStage
	}
	if context["StartTime"] != nil {
		os.Setenv("StartTime", context["StartTime"].(string))
	}
	return Stage(fmt.Sprint(context["Stage"]))
}

func getHash(data string) *string {
	hasher := md5.New()
	hasher.Write([]byte(data))
	return aws.String(hex.EncodeToString(hasher.Sum(nil)))
}

func LogPanic() {
	if r := recover(); r != nil {
		log.Println(string(debug.Stack()))
		panic(r)
	}
}

func getLocalKubeConfig() ([]byte, error) {
	data, err := ioutil.ReadFile(KubeConfigLocalPath)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func isZero(v reflect.Value) bool {
	if !v.IsValid() {
		return true
	}

	switch v.Kind() {
	case reflect.Bool:
		return v.Bool() == false

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int() == 0

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32,
		reflect.Uint64, reflect.Uintptr:
		return v.Uint() == 0

	case reflect.Float32, reflect.Float64:
		return v.Float() == 0

	case reflect.Ptr, reflect.Interface:
		return isZero(v.Elem())

	case reflect.Complex64, reflect.Complex128:
		return v.Complex() == 0

	case reflect.Array:
		for i := 0; i < v.Len(); i++ {
			if !isZero(v.Index(i)) {
				return false
			}
		}
		return true

	case reflect.Slice, reflect.String, reflect.Map:
		return v.Len() == 0

	case reflect.Struct:
		for i, n := 0, v.NumField(); i < n; i++ {
			if !isZero(v.Field(i)) {
				return false
			}
		}
		return true
	default:
		return v.IsNil()
	}
}

// IsZero to check is the nil or zero value
func IsZero(v interface{}) bool {
	return isZero(reflect.ValueOf(v))
}

func roughlyEqual(a []*string, b []*string) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	for _, i := range a {
		matched := false
		for _, ii := range b {
			if i == nil && ii == nil {
				matched = true
				break
			}
			if i == nil || ii == nil {
				continue
			}
			if *ii == *i {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	for _, i := range b {
		matched := false
		for _, ii := range a {
			if *ii == *i {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

// checkSize to see if the size of interface is greater than
func checkSize(v interface{}, size int) bool {
	gob.Register(map[string]interface{}{})
	b := new(bytes.Buffer)
	if err := gob.NewEncoder(b).Encode(v); err != nil {
		//log.Printf("Warning: Error calculating size of output: %s", err.Error())
		return false
	}
	if b.Len() >= size {
		return true
	}
	return false
}

// ScanFromStruct scan specific fields from struct
func ScanFromStruct(v interface{}, name string) (interface{}, bool) {
	var temp interface{}
	for i, k := range strings.Split(name, ".") {
		if i == 0 {
			temp = v
		}
		temp = scanFromStruct(reflect.ValueOf(temp), k)
		if IsZero(temp) {
			break
		}
		gob.Register(temp)
	}
	if IsZero(temp) {
		return nil, false
	}
	return temp, true
}

func scanFromStruct(s reflect.Value, name string) interface{} {
	typeOfT := s.Type()
	switch s.Kind() {
	case reflect.Struct:
		for i := 0; i < s.NumField(); i++ {
			f := s.Field(i)
			if typeOfT.Field(i).Name == name {
				return f.Interface()
			}
		}
		return nil
	case reflect.Ptr:
		return scanFromStruct(s.Elem(), name)
	}
	return s.Interface()
}

func stringInSlice(a string, list []string) bool {
	for _, b := range list {
		if b == a {
			return true
		}
	}
	return false
}

func structToMap(item interface{}) map[string]interface{} {
	res := map[string]interface{}{}
	if item == nil {
		return res
	}
	v := reflect.TypeOf(item)
	reflectValue := reflect.ValueOf(item)
	reflectValue = reflect.Indirect(reflectValue)

	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	for i := 0; i < v.NumField(); i++ {
		tag := v.Field(i).Tag.Get("json")
		if reflectValue.Field(i).CanInterface() {
			field := reflectValue.Field(i).Interface()
			keyName := tag
			if tag != "" && tag != "-" {
				if index := strings.Index(tag, ","); index != -1 {
					if strings.Index(tag[index+1:], "omitempty") != -1 && IsZero(field) {
						continue
					}
					keyName = v.Field(i).Name
				}
				switch v.Field(i).Type.Kind() {
				case reflect.Struct:
					res[keyName] = structToMap(field)
				default:
					res[keyName] = stringify(field)
				}
			}
		}
	}
	return res
}

func stringify(v interface{}) interface{} {
	val := reflect.ValueOf(v)
	switch val.Kind() {
	case reflect.String, reflect.Bool, reflect.Int, reflect.Int32, reflect.Int64, reflect.Float64:
		return fmt.Sprint(v)
	case reflect.Map:
		out := make(map[string]interface{})
		for _, key := range val.MapKeys() {
			v := stringify(val.MapIndex(key).Interface())
			out[key.String()] = v
		}
		return out
	case reflect.Slice:
		out := make([]interface{}, val.Len())
		for i := 0; i < val.Len(); i++ {
			v := stringify(val.Index(i).Interface())
			out[i] = v
		}
		return out
	case reflect.Struct:
		return structToMap(v)
	case reflect.Ptr:
		if val.IsNil() {
			return nil
		}
		return stringify(val.Elem().Interface())
	default:
		fmt.Println("Unsupported type in stringify " + val.Kind().String())
		return nil
	}
}

// pushLastKnownError to push to slice of string to send ot CFN
func pushLastKnownError(msg string) {
	if !stringInSlice(msg, LastKnownErrors) {
		LastKnownErrors = append(LastKnownErrors, msg)
	}
}

// popLastKnownError to pop from the LastKnownErrors
func popLastKnownError(name string) {
	for i, v := range LastKnownErrors {
		re := regexp.MustCompile(name)
		if re.MatchString(v) {
			LastKnownErrors[i] = LastKnownErrors[len(LastKnownErrors)-1]
			LastKnownErrors[len(LastKnownErrors)-1] = ""
			LastKnownErrors = LastKnownErrors[:len(LastKnownErrors)-1]
		}
	}
}

// downloadOCI to download charts from OCI repositories
func downloadOCI(endpoint, manifest, username, password, file string) error {
	fullHost := fmt.Sprintf("%s/%s", endpoint, manifest)
	fmt.Println(fullHost)
	regClient, err := registry.NewClient()
	if err != nil {
		fmt.Println("New client")
		return err
	}

	err = regClient.Login(endpoint, registry.LoginOptBasicAuth(username, password))
	if err != nil {
		fmt.Println("Login")
		return err
	}
	defer regClient.Logout(endpoint)
	result, err := regClient.Pull(fullHost)
	if err != nil {
		fmt.Println("Pull")
		return err
	}
	err = ioutil.WriteFile(file, result.Chart.Data, 0644)
	if err != nil {
		return genericError("Writing file", err)
	}
	log.Printf("Downloaded %s ", file)

	return nil
}
