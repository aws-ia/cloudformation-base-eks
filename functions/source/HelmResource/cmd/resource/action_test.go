package resource

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/aws-cloudformation/cloudformation-cli-go-plugin/cfn/handler"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/stretchr/testify/assert"
)

func TestInitialize(t *testing.T) {
	m := &Model{
		ClusterID: aws.String("eks"),
		Chart:     aws.String("stable/coscale"),
		Namespace: aws.String("default"),
	}
	vpc := &VPCConfiguration{
		SecurityGroupIds: []string{"sg-01"},
		SubnetIds:        []string{"subnet-01"},
	}
	vpcPending := &VPCConfiguration{
		SecurityGroupIds: []string{"sg-01"},
		SubnetIds:        []string{"subnet-02"},
	}
	data := []byte("Test")
	_ = os.WriteFile(KubeConfigLocalPath, data, 0644)
	_ = os.WriteFile(ZipFile, data, 0644)
	defer os.Remove(KubeConfigLocalPath)
	defer os.Remove(ZipFile)
	tests := map[string]struct {
		action    Action
		vpc       bool
		name      string
		nextStage Stage
	}{
		"InstallWithVPC": {
			action:    InstallReleaseAction,
			name:      "test",
			vpc:       true,
			nextStage: ReleaseStabilize,
		},
		"InstallWithOutVPC": {
			action:    InstallReleaseAction,
			name:      "test",
			vpc:       false,
			nextStage: ReleaseStabilize,
		},
		"UpdateWithOutVPC": {
			action:    UpdateReleaseAction,
			name:      "one",
			vpc:       false,
			nextStage: ReleaseStabilize,
		},
		"UpdateWithVPC": {
			action:    UpdateReleaseAction,
			name:      "one",
			vpc:       true,
			nextStage: ReleaseStabilize,
		},
		"UninstallsWithOutVPC": {
			action:    UninstallReleaseAction,
			name:      "one",
			vpc:       false,
			nextStage: CompleteStage,
		},
		"UninstallWithVPC": {
			action:    UninstallReleaseAction,
			name:      "one",
			vpc:       true,
			nextStage: CompleteStage,
		},
		"Unknown": {
			action:    CheckReleaseAction,
			name:      "one",
			vpc:       false,
			nextStage: NoStage,
		},
		"PendingLambda": {
			action:    InstallReleaseAction,
			name:      "Test",
			vpc:       true,
			nextStage: LambdaStabilize,
		},
	}

	var eRes handler.ProgressEvent
	for name, d := range tests {
		t.Run(name, func(t *testing.T) {
			if d.vpc {
				m.VPCConfiguration = vpc
				if name == "PendingLambda" {
					m.VPCConfiguration = vpcPending
				}
			}
			NewClients = func(cluster *string, kubeconfig *string, namespace *string, ses *session.Session, role *string, customKubeconfig []byte, vpcConfig *VPCConfiguration) (*Clients, error) {
				return NewMockClient(t, m), nil
			}
			m.Name = aws.String(d.name)
			m.ID, _ = generateID(m, d.name, "eu-west-1", "default")
			switch name {
			case "Unknown":
				eRes = makeEvent(m, d.nextStage, NewError(ErrCodeInvalidException, fmt.Sprintf("unhandled stage %s", d.action)))
			case "UninstallsWithOutVPC", "UninstallWithVPC":
				eRes = makeEvent(nil, d.nextStage, nil)
			default:
				eRes = makeEvent(m, d.nextStage, nil)
			}
			res := initialize(MockSession, m, d.action)
			assert.EqualValues(t, eRes, res)
		})
	}
}

func TestCheckReleaseStatus(t *testing.T) {
	m := &Model{
		ClusterID: aws.String("eks"),
		ID:        aws.String("eyJDbHVzdGVySUQiOiJla3MiLCJSZWdpb24iOiJldS13ZXN0LTEiLCJOYW1lIjoiVGVzdCIsIk5hbWVzcGFjZSI6IlRlc3QifQ"),
	}
	vpc := &VPCConfiguration{
		SecurityGroupIds: []string{"sg-01"},
		SubnetIds:        []string{"subnet-01"},
	}
	vpcPending := &VPCConfiguration{
		SecurityGroupIds: []string{"sg-01"},
		SubnetIds:        []string{"subnet-02"},
	}
	data := []byte("Test")
	_ = os.WriteFile(KubeConfigLocalPath, data, 0644)
	_ = os.WriteFile(ZipFile, data, 0644)
	defer os.Remove(KubeConfigLocalPath)
	defer os.Remove(ZipFile)
	tests := map[string]struct {
		vpc       bool
		name      *string
		nextStage Stage
	}{
		"WithVPC": {
			name:      aws.String("one"),
			vpc:       true,
			nextStage: CompleteStage,
		},
		"WithOutVPC": {
			name:      aws.String("one"),
			vpc:       false,
			nextStage: CompleteStage,
		},
		"Pending": {
			name:      aws.String("five"),
			vpc:       false,
			nextStage: ReleaseStabilize,
		},
		"PendingResource": {
			name:      aws.String("three"),
			vpc:       false,
			nextStage: ReleaseStabilize,
		},
		"Unknown": {
			name:      aws.String("four"),
			vpc:       false,
			nextStage: NoStage,
		},
		"PendingLambda": {
			name:      aws.String("one"),
			vpc:       true,
			nextStage: LambdaStabilize,
		},
	}

	var eRes handler.ProgressEvent
	for name, d := range tests {
		t.Run(name, func(t *testing.T) {
			m.VPCConfiguration = nil
			NewClients = func(cluster *string, kubeconfig *string, namespace *string, ses *session.Session, role *string, customKubeconfig []byte, vpcConfig *VPCConfiguration) (*Clients, error) {
				return NewMockClient(t, m), nil
			}
			if d.vpc {
				m.VPCConfiguration = vpc
				if name == "PendingLambda" {
					m.VPCConfiguration = vpcPending
				}
			}
			m.Name = d.name
			switch name {
			case "Unknown":
				eRes = makeEvent(m, d.nextStage, NewError(ErrCodeHelmActionException, "release failed"))
			default:
				eRes = makeEvent(m, d.nextStage, nil)
			}
			res := checkReleaseStatus(MockSession, m, d.nextStage)
			assert.EqualValues(t, eRes, res)
		})
	}
}
func TestLambdaDestroy(t *testing.T) {
	m := &Model{
		ClusterID: aws.String("eks"),
		VPCConfiguration: &VPCConfiguration{
			SecurityGroupIds: []string{"sg-1"},
			SubnetIds:        []string{"subnet-1"},
		},
	}
	expected := makeEvent(nil, CompleteStage, nil)
	c := NewMockClient(t, m)
	result := c.lambdaDestroy(m)
	assert.EqualValues(t, expected, result)

}

func TestInitializeLambda(t *testing.T) {
	l := &lambdaResource{
		nameSuffix:   aws.String("suffix"),
		functionFile: TestZipFile,
		vpcConfig: &VPCConfiguration{
			SecurityGroupIds: []string{"sg-1"},
			SubnetIds:        []string{"subnet-1"},
		},
	}
	eErr := "not in desired state"
	tests := map[string]struct {
		name      *string
		assertion assert.BoolAssertionFunc
	}{
		"StateActive": {
			name:      aws.String("function1"),
			assertion: assert.True,
		},
		"StateFailed": {
			name:      aws.String("function2"),
			assertion: assert.False,
		},
		"StateNotFound": {
			name:      aws.String("Nofunct"),
			assertion: assert.False,
		},
	}
	c := NewMockClient(t, nil)
	for name, d := range tests {
		t.Run(name, func(t *testing.T) {
			l.functionName = d.name
			result, err := c.initializeLambda(l)
			if err != nil {
				assert.Contains(t, err.Error(), eErr)
			}
			d.assertion(t, result)
		})
	}
}

func TestHelmStatusWrapper(t *testing.T) {
	c := NewMockClient(t, nil)
	event := &Event{
		Action: CheckReleaseAction,
	}
	name := aws.String("one")
	tests := []bool{true, false}
	functionName := aws.String("function1")
	for _, d := range tests {
		testName := "WithOutVPC"
		if d {
			testName = "WithVPC"
		}
		t.Run(testName, func(t *testing.T) {
			_, err := c.helmStatusWrapper(name, event, functionName, d)
			assert.Nil(t, err)
		})
	}
}

func TestHelmListWrapper(t *testing.T) {
	c := NewMockClient(t, nil)
	event := &Event{
		Action: CheckReleaseAction,
		Inputs: &Inputs{
			ChartDetails: &Chart{
				Chart:     aws.String("hello-0.1.0"),
				ChartName: aws.String("hello"),
			},
			Config: &Config{
				Namespace: aws.String("default"),
			},
		},
	}

	tests := []bool{true, false}
	functionName := aws.String("function1")
	for _, d := range tests {
		testName := "WithOutVPC"
		if d {
			testName = "WithVPC"
		}
		t.Run(testName, func(t *testing.T) {
			_, err := c.helmListWrapper(event, functionName, d)
			assert.Nil(t, err)
		})
	}
}
func TestHelmInstallWrapper(t *testing.T) {
	defer os.Remove(chartLocalPath)
	testServer := httptest.NewServer(http.StripPrefix("/", http.FileServer(http.Dir(TestFolder))))
	defer func() { testServer.Close() }()
	c := NewMockClient(t, nil)
	event := &Event{
		Action: InstallReleaseAction,
		Inputs: &Inputs{
			Config: &Config{
				Name:      aws.String("test"),
				Namespace: aws.String("default"),
			},
			ValueOpts: map[string]interface{}{},
		},
		Model: &Model{
			ID: aws.String("function1"),
		},
	}
	event.Inputs.ChartDetails, _ = c.getChartDetails(&Model{Chart: aws.String(testServer.URL + "/test.tgz")})
	tests := []bool{true, false}
	functionName := aws.String("function1")
	for _, d := range tests {
		testName := "WithOutVPC"
		if d {
			testName = "WithVPC"
		}
		t.Run(testName, func(t *testing.T) {
			err := c.helmInstallWrapper(event, functionName, d)
			assert.Nil(t, err)
		})
	}
}

func TestHelmUpgradeWrapper(t *testing.T) {
	testServer := httptest.NewServer(http.StripPrefix("/", http.FileServer(http.Dir(TestFolder))))
	defer func() { testServer.Close() }()
	c := NewMockClient(t, nil)
	event := &Event{
		Action: UpdateReleaseAction,
		Inputs: &Inputs{
			Config: &Config{
				Name:      aws.String("one"),
				Namespace: aws.String("default"),
			},
			ValueOpts: map[string]interface{}{},
		},
		Model: &Model{
			ID: aws.String("umock-id"),
		},
	}
	event.Inputs.ChartDetails, _ = c.getChartDetails(&Model{Chart: aws.String(testServer.URL + "/test.tgz")})
	name := aws.String("one")
	tests := []bool{true, false}
	functionName := aws.String("function1")
	for _, d := range tests {
		testName := "WithOutVPC"
		if d {
			testName = "WithVPC"
		}
		t.Run(testName, func(t *testing.T) {
			err := c.helmUpgradeWrapper(name, event, functionName, d)
			assert.Nil(t, err)
		})
	}
}

func TestHelmDeleteWrapper(t *testing.T) {
	c := NewMockClient(t, nil)
	event := &Event{
		Action: UninstallReleaseAction,
	}
	name := aws.String("one")
	tests := []bool{true, false}
	functionName := aws.String("function1")
	for _, d := range tests {
		testName := "WithOutVPC"
		if d {
			testName = "WithVPC"
		}
		t.Run(testName, func(t *testing.T) {
			err := c.helmDeleteWrapper(name, event, functionName, d)
			assert.Nil(t, err)
		})
	}
}

func TestKubePendingWrapper(t *testing.T) {
	c := NewMockClient(t, nil)
	event := &Event{
		Action: GetPendingAction,
		ReleaseData: &ReleaseData{
			Name:      "one",
			Namespace: "default",
			Manifest:  TestManifest,
		},
	}
	tests := []bool{true, false}
	functionName := aws.String("function1")
	for _, d := range tests {
		testName := "WithOutVPC"
		if d {
			testName = "WithVPC"
		}
		t.Run(testName, func(t *testing.T) {
			_, err := c.kubePendingWrapper(event, functionName, d)
			assert.Nil(t, err)
		})
	}
}

func TestKubeResourcesWrapper(t *testing.T) {
	c := NewMockClient(t, nil)
	event := &Event{
		Action: GetResourcesAction,
		ReleaseData: &ReleaseData{
			Name:      "one",
			Namespace: "default",
			Manifest:  TestManifest,
		},
	}
	tests := []bool{true, false}
	functionName := aws.String("function1")
	for _, d := range tests {
		testName := "WithOutVPC"
		if d {
			testName = "WithVPC"
		}
		t.Run(testName, func(t *testing.T) {
			_, err := c.kubeResourcesWrapper(event, functionName, d)
			assert.Nil(t, err)
		})
	}
}
