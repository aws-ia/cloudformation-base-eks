package resource

import (
	"bytes"
	"context"
	"fmt"
	"github.com/phayes/freeport"
	"golang.org/x/crypto/bcrypt"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/chartutil"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/distribution/distribution/v3/configuration"
	"github.com/distribution/distribution/v3/registry"
	_ "github.com/distribution/distribution/v3/registry/auth/htpasswd"           // used for docker test registry
	_ "github.com/distribution/distribution/v3/registry/storage/driver/inmemory" // used for docker test registry
	"github.com/stretchr/testify/assert"
	ociRegistry "helm.sh/helm/v3/pkg/registry"
)

type TestDetailParam struct {
	ID int `json:",omitempty"`
}

type TestDetailSubStructure struct {
	ID     int               `json:",omitempty"`
	Params []TestDetailParam `json:",omitempty"`
}

type TestDetail struct {
	ID     int                    `json:",omitempty"`
	Detail Detail                 `json:",omitempty"`
	Data   TestDetailSubStructure `json:",omitempty"`
}

type Detail interface{}

type OCIServer struct {
	RegistryURL  string
	TestUsername string
	TestPassword string
	Chart        string
}

// TestMergeMaps is to test MergeMaps
func TestMergeMaps(t *testing.T) {
	m1 := map[string]interface{}{
		"a": "a",
		"b": "a",
	}
	m2 := map[string]interface{}{
		"a": "b",
		"b": "b",
		"c": "c",
	}
	expectedMap := map[string]interface{}{
		"a": "b",
		"b": "b",
		"c": "c",
	}
	result := mergeMaps(m1, m2)
	assert.EqualValues(t, expectedMap, result)
}

func TestProcessValues(t *testing.T) {
	stringYaml := `root:
  firstlevel: value
  secondlevel:
    - a1
    - a2
  string: true`
	tests := map[string]struct {
		m    *Model
		eRes map[string]interface{}
		eErr string
	}{
		"CorrectValues": {
			m: &Model{
				Values:           map[string]string{"stack.nested": "true"},
				ValueYaml:        aws.String(stringYaml),
				ValueOverrideURL: aws.String("s3://test/test.yaml"),
			},
			eRes: map[string]interface{}{"root": map[string]interface{}{"file": true, "firstlevel": "value", "secondlevel": []interface{}{"a1", "a2"}, "string": true}, "stack": map[string]interface{}{"nested": true}},
		},
		"WrongYaml": {
			m: &Model{
				ValueYaml: aws.String("stringYaml"),
			},
			eErr: "error unmarshaling JSON",
		},
		"WrongPath": {
			m: &Model{
				ValueOverrideURL: aws.String("../test"),
			},
			eErr: "InvalidParameter",
		},
	}
	data, _ := ioutil.ReadFile(TestFolder + "/test.yaml")
	_, _ = dlLoggingSvcNoChunk(data)

	c := NewMockClient(t, nil)
	for name, d := range tests {
		t.Run(name, func(t *testing.T) {
			result, err := c.processValues(d.m)
			if err != nil {
				assert.Contains(t, err.Error(), d.eErr)
			}
			assert.EqualValues(t, d.eRes, result)
		})
	}
}

// TestGetChartDetails is to test getChartDetails
func TestGetChartDetails(t *testing.T) {
	tests := map[string]struct {
		m             *Model
		expectedChart *Chart
		expectedError *string
	}{
		"test1": {
			m: &Model{
				Chart:      aws.String("stable/test"),
				Repository: aws.String("test.com"),
			},
			expectedChart: &Chart{
				Chart:              aws.String("stable/test"),
				ChartRepo:          aws.String("stable"),
				ChartName:          aws.String("test"),
				ChartType:          aws.String("Remote"),
				ChartRepoURL:       aws.String("test.com"),
				ChartSkipTLSVerify: aws.Bool(false),
				ChartLocalCA:       aws.Bool(false),
			},
			expectedError: nil,
		},
		"test2": {
			m: &Model{
				Repository: aws.String("test.com"),
			},
			expectedChart: &Chart{},
			expectedError: aws.String("chart is required"),
		},
		"test3": {
			m: &Model{
				Chart:   aws.String("test"),
				Version: aws.String("1.0.0"),
			},
			expectedChart: &Chart{
				Chart:              aws.String("stable/test"),
				ChartRepo:          aws.String("stable"),
				ChartName:          aws.String("test"),
				ChartType:          aws.String("Remote"),
				ChartRepoURL:       aws.String("https://charts.helm.sh/stable"),
				ChartVersion:       aws.String("1.0.0"),
				ChartSkipTLSVerify: aws.Bool(false),
				ChartLocalCA:       aws.Bool(false),
			},
			expectedError: nil,
		},
		"test4": {
			m: &Model{
				Chart: aws.String("s3://test/chart-1.0.1.tgz"),
			},
			expectedChart: &Chart{
				Chart:        aws.String("/tmp/chart.tgz"),
				ChartName:    aws.String("chart"),
				ChartType:    aws.String("Local"),
				ChartPath:    aws.String("s3://test/chart-1.0.1.tgz"),
				ChartRepoURL: aws.String("https://charts.helm.sh/stable"),
			},
		},
		"test5": {
			m: &Model{
				Chart:      aws.String("stable/test"),
				Repository: aws.String("test.com"),
				RepositoryOptions: &RepositoryOptions{
					Username:              aws.String("test"),
					Password:              aws.String("tester"),
					InsecureSkipTLSVerify: aws.Bool(true),
					CAFile:                aws.String("s3://buctket/key"),
				},
			},
			expectedChart: &Chart{
				Chart:              aws.String("stable/test"),
				ChartRepo:          aws.String("stable"),
				ChartName:          aws.String("test"),
				ChartType:          aws.String("Remote"),
				ChartRepoURL:       aws.String("test.com"),
				ChartUsername:      aws.String("test"),
				ChartPassword:      aws.String("tester"),
				ChartSkipTLSVerify: aws.Bool(true),
				ChartLocalCA:       aws.Bool(true),
			},
			expectedError: nil,
		},
		"test6": {
			m: &Model{
				Chart:      aws.String("stable/test"),
				Repository: aws.String("test.com"),
				RepositoryOptions: &RepositoryOptions{
					Username: aws.String("test"),
				},
			},
			expectedChart: &Chart{
				Chart:              aws.String("stable/test"),
				ChartRepo:          aws.String("stable"),
				ChartName:          aws.String("test"),
				ChartType:          aws.String("Remote"),
				ChartRepoURL:       aws.String("test.com"),
				ChartSkipTLSVerify: aws.Bool(false),
				ChartLocalCA:       aws.Bool(false),
			},
			expectedError: nil,
		},
	}
	c := NewMockClient(t, nil)
	for name, d := range tests {
		t.Run(name, func(t *testing.T) {
			result, err := c.getChartDetails(d.m)
			if err != nil {
				assert.EqualError(t, err, aws.StringValue(d.expectedError))
			} else {
				assert.EqualValues(t, d.expectedChart, result)
			}
		})
	}
}

// TestGetReleaseName is to test getReleaseName
func TestGetReleaseName(t *testing.T) {
	tests := map[string]struct {
		name         *string
		chartname    *string
		expectedName *string
	}{
		"NameProvided": {
			name:         aws.String("Test"),
			chartname:    nil,
			expectedName: aws.String("Test"),
		},
		"AllValues": {
			name:         aws.String("Test"),
			chartname:    aws.String("TestChart"),
			expectedName: aws.String("Test"),
		},
		"OnlyChart": {
			name:         nil,
			chartname:    aws.String("TestChart"),
			expectedName: aws.String("TestChart-" + fmt.Sprint(time.Now().Unix())),
		},
		"NoValues": {
			name:         nil,
			chartname:    nil,
			expectedName: nil,
		},
	}
	for name, d := range tests {
		t.Run(name, func(t *testing.T) {
			result := getReleaseName(d.name, d.chartname)
			assert.EqualValues(t, aws.StringValue(d.expectedName), aws.StringValue(result))
		})
	}
}

// TestGetReleaseNameContextis to test getReleaseNameContext
func TestGetReleaseNameContext(t *testing.T) {
	tests := map[string]struct {
		context      map[string]interface{}
		expectedName *string
	}{
		"NameProvided": {
			context:      map[string]interface{}{"Name": "Test"},
			expectedName: aws.String("Test"),
		},
		"Nil": {
			context:      map[string]interface{}{},
			expectedName: nil,
		},
		"NoValues": {
			context:      map[string]interface{}{"StartTime": "Testtime"},
			expectedName: nil,
		},
	}
	for name, d := range tests {
		t.Run(name, func(t *testing.T) {
			result := getReleaseNameContext(d.context)
			assert.EqualValues(t, aws.StringValue(d.expectedName), aws.StringValue(result))
		})
	}
}

// TestGetReleaseNameSpace is to test getReleaseNameSpace
func TestGetReleaseNameSpace(t *testing.T) {
	tests := map[string]struct {
		namespace         *string
		expectedNamespace *string
	}{
		"NameProvided": {
			namespace:         aws.String("default"),
			expectedNamespace: aws.String("default"),
		},
		"NoValues": {
			namespace:         nil,
			expectedNamespace: aws.String("default"),
		},
	}
	for name, d := range tests {
		t.Run(name, func(t *testing.T) {
			result := getReleaseNameSpace(d.namespace)
			assert.EqualValues(t, aws.StringValue(d.expectedNamespace), aws.StringValue(result))
		})
	}
}

// TestHTTPDownload is to test downloadHTTP
func TestHTTPDownload(t *testing.T) {
	files := []string{"test.tgz", "nonExt"}
	testServer := httptest.NewServer(http.StripPrefix("/", http.FileServer(http.Dir(TestFolder))))
	defer func() { testServer.Close() }()
	for _, file := range files {
		t.Run(file, func(t *testing.T) {

			err := downloadHTTP(testServer.URL+"/"+file, "/dev/null", nil, nil)
			if err != nil {
				assert.Contains(t, err.Error(), "At Downloading file")
			}
		})
	}
}

// TestGenerateID is to test generateID
func TestGenerateID(t *testing.T) {
	eID := aws.String("eyJDbHVzdGVySUQiOiJla3MiLCJSZWdpb24iOiJldS13ZXN0LTEiLCJOYW1lIjoiVGVzdCIsIk5hbWVzcGFjZSI6ImRlZmF1bHQifQ")
	tests := map[string]struct {
		m                                      Model
		name, region, namespace, expectedError string
		expectedID                             *string
	}{
		"WithAllValues": {
			m: Model{
				ClusterID:  aws.String("eks"),
				KubeConfig: aws.String("arn"),
			},
			name:          "Test",
			region:        "eu-west-1",
			namespace:     "default",
			expectedID:    eID,
			expectedError: "both ClusterID or KubeConfig can not be specified",
		},
		"NoModelValues": {
			m: Model{
				ClusterID:  nil,
				KubeConfig: nil,
			},
			name:          "Test",
			region:        "eu-west-1",
			namespace:     "default",
			expectedID:    eID,
			expectedError: "either ClusterID or KubeConfig must be specified",
		},
		"BlankName": {
			m: Model{
				ClusterID:  aws.String("eks"),
				KubeConfig: nil,
			},
			name:          "",
			region:        "eu-west-1",
			namespace:     "default",
			expectedID:    eID,
			expectedError: "incorrect values for variable name, namespace, region",
		},
		"BlankValues": {
			m: Model{
				ClusterID:  nil,
				KubeConfig: nil,
			},
			name:          "",
			region:        "",
			namespace:     "",
			expectedID:    eID,
			expectedError: "either ClusterID or KubeConfig must be specified",
		},
		"CorrectValues": {
			m: Model{
				ClusterID:  aws.String("eks"),
				KubeConfig: nil,
			},
			name:          "Test",
			region:        "eu-west-1",
			namespace:     "default",
			expectedID:    eID,
			expectedError: "",
		},
		"CorrectValuesWithVPC": {
			m: Model{
				ClusterID:  aws.String("eks"),
				KubeConfig: nil,
				VPCConfiguration: &VPCConfiguration{
					SecurityGroupIds: []string{"sg-01"},
					SubnetIds:        []string{"subnet-01"},
				},
			},
			name:          "Test",
			region:        "eu-west-1",
			namespace:     "default",
			expectedID:    aws.String("eyJDbHVzdGVySUQiOiJla3MiLCJSZWdpb24iOiJldS13ZXN0LTEiLCJOYW1lIjoiVGVzdCIsIk5hbWVzcGFjZSI6ImRlZmF1bHQiLCJWUENDb25maWd1cmF0aW9uIjp7IlNlY3VyaXR5R3JvdXBJZHMiOlsic2ctMDEiXSwiU3VibmV0SWRzIjpbInN1Ym5ldC0wMSJdfX0"),
			expectedError: "",
		},
	}
	for name, d := range tests {
		t.Run(name, func(t *testing.T) {
			result, err := generateID(&d.m, d.name, d.region, d.namespace)
			if err != nil {
				assert.EqualError(t, err, d.expectedError)
			} else {
				t.Log(aws.StringValue(result))
				assert.EqualValues(t, aws.StringValue(d.expectedID), aws.StringValue(result))
			}
		})
	}
}

// TestDecodeID is to test DecodeID
func TestDecodeID(t *testing.T) {
	sIDs := []*string{aws.String("eyJDbHVzdGVySUQiOiJla3MiLCJSZWdpb24iOiJldS13ZXN0LTEiLCJOYW1lIjoiVGVzdCIsIk5hbWVzcGFjZSI6IlRlc3QifQ"), aws.String("wrong")}
	eID := &ID{
		ClusterID: aws.String("eks"),
		Name:      aws.String("Test"),
		Region:    aws.String("eu-west-1"),
		Namespace: aws.String("Test"),
	}
	eErr := "illegal base64 data"
	for _, sID := range sIDs {
		t.Run("test", func(t *testing.T) {
			result, err := DecodeID(sID)
			if err != nil {
				assert.Contains(t, err.Error(), eErr)
			} else {
				assert.EqualValues(t, eID, result)
			}
		})
	}
}

// TestDownloadChart is to test downloadChart
func TestDownloadChart(t *testing.T) {
	testServer := httptest.NewServer(http.StripPrefix("/", http.FileServer(http.Dir(TestFolder))))
	defer func() { testServer.Close() }()
	files := []string{testServer.URL + "/test.tgz", "s3://buctket/key"}
	c := NewMockClient(t, nil)
	for _, file := range files {
		t.Run(file, func(t *testing.T) {
			err := c.downloadChart(file, "/dev/null", nil, nil)
			assert.Nil(t, err)
		})
	}
}

// TestCheckTimeOut to test checkTimeOut
func TestCheckTimeOut(t *testing.T) {
	timeOut := aws.Int(90)
	tests := map[string]struct {
		time      string
		assertion assert.BoolAssertionFunc
	}{
		"10M": {
			time:      time.Now().Add(time.Minute * -10).Format(time.RFC3339),
			assertion: assert.False,
		},
		"10H": {
			time:      time.Now().Add(time.Hour * -10).Format(time.RFC3339),
			assertion: assert.True,
		},
	}
	for name, d := range tests {
		t.Run(name, func(t *testing.T) {
			result := checkTimeOut(d.time, timeOut)
			d.assertion(t, result)
		})
	}
}

// TestGetStage is to test getStage
func TestGetStage(t *testing.T) {
	st := time.Now().Format(time.RFC3339)
	tests := map[string]struct {
		context       map[string]interface{}
		expectedStage Stage
		expectedTime  string
	}{
		"Init": {
			context:       make(map[string]interface{}),
			expectedStage: InitStage,
			expectedTime:  st,
		},
		"Stage": {
			context: map[string]interface{}{
				"Stage":     "ReleaseStabilize",
				"StartTime": st,
			},
			expectedStage: ReleaseStabilize,
			expectedTime:  st,
		},
		"StageNotime": {
			context: map[string]interface{}{
				"Stage": "ReleaseStabilize",
			},
			expectedStage: ReleaseStabilize,
			expectedTime:  st,
		},
		"TimeNoStage": {
			context: map[string]interface{}{
				"StartTime": st,
			},
			expectedStage: InitStage,
			expectedTime:  st,
		},
	}
	for name, d := range tests {
		t.Run(name, func(t *testing.T) {
			os.Setenv("StartTime", d.expectedTime)
			result := getStage(d.context)
			assert.EqualValues(t, d.expectedStage, result)
			assert.EqualValues(t, d.expectedTime, os.Getenv("StartTime"))
		})
	}
}

// TestHash is to test getHash
func TestHash(t *testing.T) {
	str := "Test"
	expectedHash := aws.String("0cbc6611f5540bd0809a388dc95a615b")
	result := getHash(str)
	assert.EqualValues(t, aws.StringValue(expectedHash), aws.StringValue(result))
}

func TestZero(t *testing.T) {
	one, zeroInt := 1, 0

	type myString string

	var interface1, interfaceZero interface{} = &one, &zeroInt

	var (
		zeroDetail1 Detail = &struct{}{}
		zeroDetail2 Detail = &TestDetail{}
		zeroDetail3 Detail = struct{}{}
		zeroDetail4 Detail = &TestDetail{}
		zeroDetail5 Detail = &TestDetail{Data: TestDetailSubStructure{Params: nil}}
		zeroDetail6 Detail = &TestDetail{Data: TestDetailSubStructure{
			Params: make([]TestDetailParam, 0, 10)},
		}

		nonZeroDetail1 Detail = &TestDetail{Data: TestDetailSubStructure{
			Params: []TestDetailParam{TestDetailParam{55}}},
		}
		nonZeroDetail2 Detail = &TestDetail{Data: TestDetailSubStructure{ID: 1234}}
		nonZeroDetail3 Detail = &TestDetail{ID: 1234}
		nonZeroDetail4 Detail = &TestDetail{Detail: nonZeroDetail3}
	)

	for i, test := range []struct {
		v    interface{}
		want bool
	}{
		// basic types
		{0, true},
		{complex(0, 0), true},
		{1, false},
		{1.0, false},
		{true, false},
		{0.0, true},
		{"foo", false},
		{"", true},
		{int64(0), true},
		{myString(""), true},     // different types
		{myString("foo"), false}, // different types
		// slices
		{[]string{"foo"}, false},
		{[]string(nil), true},
		{[]string{}, true},
		// maps
		{map[string][]int{"foo": {1, 2, 3}}, false},
		{map[string][]int{"foo": {1, 2, 3}}, false},
		{map[string][]int{}, true},
		{map[string][]int(nil), true},
		// pointers
		{&one, false},
		{&zeroInt, true},
		{new(bytes.Buffer), true},
		// arrays
		{[...]int{1, 2, 3}, false},

		// interfaces
		{&interface1, false},
		{&interfaceZero, true},
		// special case for structures
		{zeroDetail1, true},
		{zeroDetail2, true},
		{zeroDetail3, true},
		{zeroDetail4, true},
		{zeroDetail5, true},
		{zeroDetail6, true},
		{nonZeroDetail1, false},
		{nonZeroDetail2, false},
		{nonZeroDetail3, false},
		{nonZeroDetail4, false},
	} {
		if IsZero(test.v) != test.want {
			t.Errorf("Zero(%v)[%d] = %t", test.v, i, !test.want)
		}
	}
}

func TestCheckSize(t *testing.T) {
	tests := map[string]struct {
		context   map[string]interface{}
		size      int
		assertion assert.BoolAssertionFunc
	}{
		"SizeOver": {
			context: map[string]interface{}{
				"DaemonSet": map[string]interface{}{
					"nginx-ds": map[string]interface{}{
						"Namespace": "default", "Status": map[string]interface{}{
							"NumberAvailable": "1", "UpdatedNumberScheduled": "1", "currentNumberScheduled": "0", "desiredNumberScheduled": "1", "numberMisscheduled": "0", "numberReady": "1",
						},
					},
				},
			},
			size:      128,
			assertion: assert.True,
		},
		"Correct": {
			context:   map[string]interface{}{},
			size:      128,
			assertion: assert.False,
		},
	}
	for name, d := range tests {
		t.Run(name, func(t *testing.T) {
			result := checkSize(d.context, d.size)
			d.assertion(t, result)
		})
	}
}

func TestScanFromStruct(t *testing.T) {
	var nDetail Detail = &TestDetail{Data: TestDetailSubStructure{
		Params: []TestDetailParam{TestDetailParam{55}}},
	}
	tests := map[string]struct {
		expected  interface{}
		value     string
		assertion assert.BoolAssertionFunc
	}{
		"Correct": {
			expected:  []TestDetailParam{TestDetailParam{55}},
			value:     "Data.Params",
			assertion: assert.True,
		},
		"NoValue": {
			expected:  nil,
			value:     "Test",
			assertion: assert.False,
		},
	}
	for name, d := range tests {
		t.Run(name, func(t *testing.T) {
			result, ok := ScanFromStruct(nDetail, d.value)
			d.assertion(t, ok)
			assert.EqualValues(t, d.expected, result)
		})
	}
}

func TestStructToMap(t *testing.T) {
	var nDetail Detail = &TestDetail{Data: TestDetailSubStructure{
		Params: []TestDetailParam{TestDetailParam{55}}},
	}
	expectedMap := map[string]interface{}{"Data": map[string]interface{}{"Params": []interface{}{map[string]interface{}{"ID": "55"}}}}
	result := structToMap(nDetail)
	assert.EqualValues(t, expectedMap, result)
}

func TestStringify(t *testing.T) {
	var nDetail Detail = &TestDetail{Data: TestDetailSubStructure{
		Params: []TestDetailParam{TestDetailParam{55}}},
	}
	expectedMap := map[string]interface{}{"Data": map[string]interface{}{"Params": []interface{}{map[string]interface{}{"ID": "55"}}}}
	result := stringify(nDetail)
	assert.EqualValues(t, expectedMap, result)
}

func TestPushLastKnownError(t *testing.T) {
	tests := map[string]struct {
		expected []string
		msg      string
	}{
		"Correct": {
			expected: []string{"Test", "Test2"},
			msg:      "Test2",
		},
		"Duplicate": {
			expected: []string{"Test"},
			msg:      "Test",
		},
	}
	for name, d := range tests {
		LastKnownErrors = []string{"Test"}
		t.Run(name, func(t *testing.T) {
			pushLastKnownError(d.msg)
			assert.EqualValues(t, d.expected, LastKnownErrors)
		})
	}
}

func TestPopLastKnownError(t *testing.T) {
	tests := map[string]struct {
		expected []string
		msg      string
	}{
		"Correct": {
			expected: []string{},
			msg:      "Test",
		},
		"Incorrect": {
			expected: []string{"Test"},
			msg:      "Test2",
		},
	}
	for name, d := range tests {
		LastKnownErrors = []string{"Test"}
		t.Run(name, func(t *testing.T) {
			popLastKnownError(d.msg)
			assert.EqualValues(t, d.expected, LastKnownErrors)
		})
	}
}

func NewOCIServerWithChart(t *testing.T) (*OCIServer, error) {
	testHtpasswdFileBasename := "authtest.htpasswd"
	testUsername, testPassword := "username", "password"

	pwBytes, err := bcrypt.GenerateFromPassword([]byte(testPassword), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal("error generating bcrypt password for test htpasswd file")
	}
	htpasswdPath := filepath.Join(TestFolder, testHtpasswdFileBasename)
	err = ioutil.WriteFile(htpasswdPath, []byte(fmt.Sprintf("%s:%s\n", testUsername, string(pwBytes))), 0644)
	if err != nil {
		t.Fatalf("error creating test htpasswd file")
	}

	defer os.Remove(testHtpasswdFileBasename)

	// Registry config
	config := &configuration.Configuration{}
	port, err := freeport.GetFreePort()
	if err != nil {
		t.Fatalf("error finding free port for test registry")
	}

	config.HTTP.Addr = fmt.Sprintf(":%d", port)
	config.HTTP.DrainTimeout = time.Duration(10) * time.Second
	config.Storage = map[string]configuration.Parameters{"inmemory": map[string]interface{}{}}
	config.Auth = configuration.Auth{
		"htpasswd": configuration.Parameters{
			"realm": "localhost",
			"path":  htpasswdPath,
		},
	}

	r, err := registry.NewRegistry(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	go r.ListenAndServe()

	registryURL := fmt.Sprintf("localhost:%d", port)

	credentialsFile := filepath.Join(TestFolder, "config.json")
	defer os.Remove(credentialsFile)

	// init test client
	registryClient, err := ociRegistry.NewClient(
		ociRegistry.ClientOptDebug(true),
		ociRegistry.ClientOptWriter(os.Stdout),
		ociRegistry.ClientOptCredentialsFile(credentialsFile),
	)
	if err != nil {
		t.Fatalf("error creating registry client")
	}

	err = registryClient.Login(
		registryURL,
		ociRegistry.LoginOptBasicAuth(testUsername, testPassword),
		ociRegistry.LoginOptInsecure(false))
	if err != nil {
		t.Fatalf("error logging into registry with good credentials")
	}

	ref := fmt.Sprintf("%s/u/ocitestuser/oci-dependent-chart:0.1.0", registryURL)

	err = chartutil.ExpandFile(TestFolder, filepath.Join(TestFolder, "oci-dependent-chart-0.1.0.tgz"))
	if err != nil {
		t.Fatal(err)
	}

	// valid chart
	ch, err := loader.LoadDir(filepath.Join(TestFolder, "oci-dependent-chart"))
	if err != nil {
		t.Fatal("error loading chart")
	}

	err = os.RemoveAll(filepath.Join(TestFolder, "oci-dependent-chart"))
	if err != nil {
		t.Fatal("error removing chart before push")
	}

	// save it back to disk..
	absPath, err := chartutil.Save(ch, TestFolder)
	if err != nil {
		t.Fatal("could not create chart archive")
	}

	// load it into memory...
	contentBytes, err := ioutil.ReadFile(absPath)
	if err != nil {
		t.Fatal("could not load chart into memory")
	}

	_, err = registryClient.Push(contentBytes, ref)
	if err != nil {
		t.Fatalf("error pushing dependent chart: %s", err)
	}
	return &OCIServer{
		RegistryURL:  registryURL,
		TestUsername: testUsername,
		TestPassword: testPassword,
		Chart:        "u/ocitestuser/oci-dependent-chart:0.1.0",
	}, nil
}

func TestDownloadOCI(t *testing.T) {
	testServer, err := NewOCIServerWithChart(t)
	if err != nil {
		t.Fatal(err)
	}
	t.Run("OCI", func(t *testing.T) {
		err := downloadOCI(testServer.RegistryURL, testServer.Chart, testServer.TestUsername, testServer.TestPassword, "/dev/null")
		assert.Nil(t, err)
	})
}
