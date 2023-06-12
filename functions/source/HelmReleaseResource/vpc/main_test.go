package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/aws-quickstart/quickstart-helm-resource-provider/cmd/resource"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/stretchr/testify/assert"
)

func TestHandler(t *testing.T) {
	defer os.Remove("/tmp/chart.tgz")
	testFolder := "../cmd/resource/testdata"
	testServer := httptest.NewServer(http.StripPrefix("/", http.FileServer(http.Dir(testFolder))))
	defer func() { testServer.Close() }()
	event := resource.Event{
		Inputs: &resource.Inputs{
			Config: &resource.Config{
				Name:      aws.String("test"),
				Namespace: aws.String("default"),
			},
			ValueOpts: map[string]interface{}{},
			ChartDetails: &resource.Chart{
				ChartType: aws.String("Local"),
				ChartPath: aws.String(testServer.URL + "/test.tgz"),
				Chart:     aws.String("/tmp/chart.tgz"),
				ChartName: aws.String("hello"),
			},
		},
		ReleaseData: &resource.ReleaseData{
			Name:      "one",
			Namespace: "default",
			Manifest:  resource.TestManifest,
		},
	}
	tests := map[string]struct {
		m      *resource.Model
		action resource.Action
		eError *string
	}{
		"InstallReleaseAction": {
			m: &resource.Model{
				ID: aws.String("eyJDbHVzdGVySUQiOiJla3MiLCJSZWdpb24iOiJldS13ZXN0LTEiLCJOYW1lIjoiVGVzdCIsIk5hbWVzcGFjZSI6ImRlZmF1bHQifQ"),
			},
			action: resource.InstallReleaseAction,
		},
		"CheckReleaseAction": {
			m: &resource.Model{
				ID: aws.String("eyJDbHVzdGVySUQiOiJla3MiLCJSZWdpb24iOiJldS13ZXN0LTEiLCJOYW1lIjoib25lIiwiTmFtZXNwYWNlIjoiZGVmYXVsdCJ9"),
			},
			action: resource.CheckReleaseAction,
		},
		"GetPendingAction": {
			m: &resource.Model{
				ID: aws.String("eyJDbHVzdGVySUQiOiJla3MiLCJSZWdpb24iOiJldS13ZXN0LTEiLCJOYW1lIjoib25lIiwiTmFtZXNwYWNlIjoiZGVmYXVsdCJ9"),
			},
			action: resource.GetPendingAction,
		},
		"GetResourcesAction": {
			m: &resource.Model{
				ID: aws.String("eyJDbHVzdGVySUQiOiJla3MiLCJSZWdpb24iOiJldS13ZXN0LTEiLCJOYW1lIjoib25lIiwiTmFtZXNwYWNlIjoiZGVmYXVsdCJ9"),
			},
			action: resource.GetResourcesAction,
		},
		"UpdateReleaseAction": {
			m: &resource.Model{
				ID: aws.String("eyJDbHVzdGVySUQiOiJla3MiLCJSZWdpb24iOiJldS13ZXN0LTEiLCJOYW1lIjoib25lIiwiTmFtZXNwYWNlIjoiZGVmYXVsdCJ9"),
			},
			action: resource.UpdateReleaseAction,
		},
		"UninstallReleaseAction": {
			m: &resource.Model{
				ID: aws.String("eyJDbHVzdGVySUQiOiJla3MiLCJSZWdpb24iOiJldS13ZXN0LTEiLCJOYW1lIjoib25lIiwiTmFtZXNwYWNlIjoiZGVmYXVsdCJ9"),
			},
			action: resource.UninstallReleaseAction,
		},
		"ListReleaseAction": {
			m: &resource.Model{
				ID: aws.String("eyJDbHVzdGVySUQiOiJla3MiLCJSZWdpb24iOiJldS13ZXN0LTEiLCJOYW1lIjoib25lIiwiTmFtZXNwYWNlIjoiZGVmYXVsdCJ9"),
			},
			action: resource.ListReleaseAction,
		},
		"Unknown": {
			m: &resource.Model{
				ID: aws.String("eyJDbHVzdGVySUQiOiJla3MiLCJSZWdpb24iOiJldS13ZXN0LTEiLCJOYW1lIjoib25lIiwiTmFtZXNwYWNlIjoiZGVmYXVsdCJ9"),
			},
			action: "Unknown",
			eError: aws.String("Unhandled stage"),
		},
		"DecodeError": {
			m: &resource.Model{
				ID: aws.String("test"),
			},
			action: resource.ListReleaseAction,
			eError: aws.String("At Json Unmarshal"),
		},
	}
	resource.NewClients = func(cluster *string, kubeconfig *string, namespace *string, ses *session.Session, role *string, customKubeconfig []byte, vpcConfig *resource.VPCConfiguration) (*resource.Clients, error) {
		return resource.NewMockClient(t, nil), nil
	}
	for name, d := range tests {
		t.Run(name, func(t *testing.T) {
			event.Model = d.m
			event.Action = d.action
			_, err := HandleRequest(context.Background(), event)
			if err != nil {
				assert.Contains(t, err.Error(), aws.StringValue(d.eError))
			}
		})
	}
}
