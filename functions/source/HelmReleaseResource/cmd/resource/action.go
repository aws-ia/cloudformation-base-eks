package resource

import (
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"

	"github.com/aws-cloudformation/cloudformation-cli-go-plugin/cfn/handler"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"helm.sh/helm/v3/pkg/release"
)

type Stage string

const (
	InitStage        Stage = "Init"
	ReleaseStabilize Stage = "ReleaseStabilize"
	UninstallRelease Stage = "UninstallRelease"
	LambdaStabilize  Stage = "LambdaStabilize"
	CompleteStage    Stage = "Complete"
	NoStage          Stage = "NoStage"
)

const (
	retryCount = 3
)

func initialize(session *session.Session, currentModel *Model, action Action) handler.ProgressEvent {
	vpc := false
	var err error
	client, err := NewClients(currentModel.ClusterID, currentModel.KubeConfig, currentModel.Namespace, session, currentModel.RoleArn, nil, currentModel.VPCConfiguration)
	if err != nil {
		return makeEvent(currentModel, NoStage, NewError(ErrCodeInvalidException, err.Error()))
	}
	if IsZero(currentModel.VPCConfiguration) && currentModel.ClusterID != nil {
		currentModel.VPCConfiguration, err = getVpcConfig(client.AWSClients.EKSClient(nil, nil), client.AWSClients.EC2Client(nil, nil), currentModel)
		if err != nil {
			return makeEvent(currentModel, NoStage, NewError(ErrCodeInvalidException, err.Error()))
		}
		// generate lambda resource when auto detected vpc configs
		if !IsZero(currentModel.VPCConfiguration) {
			client.LambdaResource = newLambdaResource(client.AWSClients.STSClient(nil, nil), currentModel.ClusterID, currentModel.KubeConfig, currentModel.VPCConfiguration)
		}
	}
	e := &Event{}
	e.Inputs = new(Inputs)
	e.Inputs.Config = new(Config)
	e.Action = action
	e.Model = currentModel
	e.Inputs.ChartDetails, err = client.getChartDetails(currentModel)
	if err != nil {
		return makeEvent(currentModel, NoStage, NewError(ErrCodeInvalidException, err.Error()))
	}
	e.Inputs.Config.Name = getReleaseName(currentModel.Name, e.Inputs.ChartDetails.ChartName)
	currentModel.Name = e.Inputs.Config.Name
	e.Inputs.Config.Namespace = getReleaseNameSpace(currentModel.Namespace)
	if currentModel.ID == nil {
		currentModel.ID, err = generateID(currentModel, *e.Inputs.Config.Name, aws.StringValue(session.Config.Region), *e.Inputs.Config.Namespace)
		if err != nil {
			return makeEvent(currentModel, NoStage, NewError(ErrCodeInvalidException, err.Error()))
		}
		return makeEvent(currentModel, InitStage, nil)
	}
	if !IsZero(currentModel.VPCConfiguration) {
		vpc = true
		e.Kubeconfig, err = getLocalKubeConfig()
		if err != nil {
			return makeEvent(currentModel, NoStage, NewError(ErrCodeKubeException, err.Error()))
		}
		u, err := client.initializeLambda(client.LambdaResource)
		if err != nil {
			return makeEvent(currentModel, NoStage, NewError(ErrCodeLambdaException, err.Error()))
		}
		if !u {
			return makeEvent(currentModel, LambdaStabilize, nil)
		}
	}
	switch e.Action {
	case InstallReleaseAction:
		e.Inputs.ValueOpts, err = client.processValues(currentModel)
		if err != nil {
			return makeEvent(currentModel, NoStage, NewError(ErrCodeInvalidException, err.Error()))
		}
		data, err := DecodeID(currentModel.ID)
		if err != nil {
			return makeEvent(currentModel, NoStage, NewError(ErrCodeInvalidException, err.Error()))
		}
		currentModel.Name = data.Name
		e.Model = currentModel
		err = client.helmInstallWrapper(e, client.LambdaResource.functionName, vpc)
		if err != nil && !strings.Contains(err.Error(), ReleaseAlreadyExistsMsg) {
			return makeEvent(currentModel, NoStage, NewError(ErrCodeHelmActionException, err.Error()))
		}
		return makeEvent(currentModel, ReleaseStabilize, nil)
	case UpdateReleaseAction:
		e.Inputs.ValueOpts, err = client.processValues(currentModel)
		if err != nil {
			return makeEvent(currentModel, NoStage, NewError(ErrCodeInvalidException, err.Error()))
		}
		data, err := DecodeID(currentModel.ID)
		if err != nil {
			return makeEvent(currentModel, NoStage, NewError(ErrCodeInvalidException, err.Error()))
		}
		e.Action = UpdateReleaseAction
		err = client.helmUpgradeWrapper(data.Name, e, client.LambdaResource.functionName, vpc)
		if err != nil {
			re := regexp.MustCompile(ErrCodeNotFound)
			if re.MatchString(err.Error()) {
				return makeEvent(nil, NoStage, NewError(ErrCodeNotFound, err.Error()))
			}
			return makeEvent(currentModel, NoStage, NewError(ErrCodeHelmActionException, err.Error()))
		}
		currentModel.Name = data.Name
		return makeEvent(currentModel, ReleaseStabilize, nil)
	case UninstallReleaseAction:
		data, err := DecodeID(currentModel.ID)
		if err != nil {
			return makeEvent(nil, NoStage, NewError(ErrCodeHelmActionException, err.Error()))
		}
		err = client.helmDeleteWrapper(data.Name, e, client.LambdaResource.functionName, vpc)
		if err != nil {
			re := regexp.MustCompile(ErrCodeNotFound)
			if re.MatchString(err.Error()) {
				return makeEvent(nil, NoStage, NewError(ErrCodeNotFound, err.Error()))
			}
			return makeEvent(nil, NoStage, NewError(ErrCodeHelmActionException, err.Error()))
		}
		return client.lambdaDestroy(currentModel)
	}
	return makeEvent(currentModel, NoStage, NewError(ErrCodeInvalidException, fmt.Sprintf("unhandled stage %s", action)))
}

func checkReleaseStatus(session *session.Session, currentModel *Model, successStage Stage) handler.ProgressEvent {
	vpc := false
	var err error
	client, err := NewClients(currentModel.ClusterID, currentModel.KubeConfig, currentModel.Namespace, session, currentModel.RoleArn, nil, currentModel.VPCConfiguration)
	if err != nil {
		return makeEvent(currentModel, NoStage, NewError(ErrCodeInvalidException, err.Error()))
	}
	if IsZero(currentModel.VPCConfiguration) && currentModel.ClusterID != nil {
		currentModel.VPCConfiguration, err = getVpcConfig(client.AWSClients.EKSClient(nil, nil), client.AWSClients.EC2Client(nil, nil), currentModel)
		if err != nil {
			return makeEvent(currentModel, NoStage, NewError(ErrCodeInvalidException, err.Error()))
		}
	}
	e := &Event{}
	e.Model = currentModel
	if !IsZero(currentModel.VPCConfiguration) {
		vpc = true
		e.Kubeconfig, err = getLocalKubeConfig()
		if err != nil {
			return makeEvent(currentModel, NoStage, NewError(ErrCodeKubeException, err.Error()))
		}
		u, err := client.initializeLambda(client.LambdaResource)
		if err != nil {
			return makeEvent(currentModel, NoStage, NewError(ErrCodeLambdaException, err.Error()))
		}
		if !u {
			return makeEvent(currentModel, LambdaStabilize, nil)
		}
	}
	e.Action = CheckReleaseAction
	s, err := client.helmStatusWrapper(currentModel.Name, e, client.LambdaResource.functionName, vpc)
	if err != nil {
		return makeEvent(currentModel, NoStage, NewError(ErrCodeHelmActionException, err.Error()))
	}
	switch s.Status {
	case release.StatusDeployed:
		e.ReleaseData = &ReleaseData{
			Name:      *currentModel.Name,
			Namespace: s.Namespace,
			Chart:     s.Chart,
			Manifest:  s.Manifest,
		}
		e.Action = GetPendingAction
		pending, err := client.kubePendingWrapper(e, client.LambdaResource.functionName, vpc)
		if err != nil {
			return makeEvent(currentModel, NoStage, NewError(ErrCodeKubeException, err.Error()))
		}
		if pending {
			log.Printf("Release %s have pending resources", e.ReleaseData.Name)
			return makeEvent(currentModel, ReleaseStabilize, nil)
		}
		log.Printf("Release %s have no pending resources.", e.ReleaseData.Name)
		return makeEvent(currentModel, successStage, nil)
	case release.StatusPendingInstall, release.StatusPendingUpgrade:
		pushLastKnownError(fmt.Sprintf("Release %s/%s in %s state", s.Namespace, *currentModel.Name, s.Status))
		return makeEvent(currentModel, ReleaseStabilize, nil)
	default:
		pushLastKnownError(fmt.Sprintf("Release %s/%s in %s state", s.Namespace, *currentModel.Name, s.Status))
		return makeEvent(currentModel, NoStage, NewError(ErrCodeHelmActionException, "release failed"))

	}
}

func (c *Clients) lambdaDestroy(currentModel *Model) handler.ProgressEvent {
	if IsZero(currentModel.VPCConfiguration) {
		return makeEvent(nil, CompleteStage, nil)
	}
	l := newLambdaResource(nil, currentModel.ClusterID, currentModel.KubeConfig, currentModel.VPCConfiguration)
	err := deleteFunction(c.AWSClients.LambdaClient(nil, nil), l.functionName)
	if err != nil {
		return makeEvent(nil, NoStage, NewError(ErrCodeLambdaException, err.Error()))
	}
	return makeEvent(nil, CompleteStage, nil)
}

func (c *Clients) initializeLambda(l *lambdaResource) (bool, error) {
	state, err := checklambdaState(c.AWSClients.LambdaClient(nil, nil), l.functionName)
	if err != nil {
		return false, err
	}
	switch state {
	case StateNotFound:
		log.Printf("VPC connector %s not found", *l.functionName)
		err := createFunction(c.AWSClients.LambdaClient(nil, nil), l)
		if err != nil {
			return false, err
		}
		count := 0
		for count < retryCount {
			state, err = checklambdaState(c.AWSClients.LambdaClient(nil, nil), l.functionName)
			if err != nil {
				return false, err
			}
			if state == StateActive {
				return true, nil
			}
			time.Sleep(5 * time.Second)
			count++
		}
		return false, nil
	case StateActive:
		var err error
		l.functionOutput, err = getFunction(c.AWSClients.LambdaClient(nil, nil), l.functionName)
		if err != nil {
			return false, err
		}
		err = updateFunction(c.AWSClients.LambdaClient(nil, nil), l)
		if err != nil {
			return false, err
		}
		return true, nil
	case StatePending:
		count := 0
		for count < retryCount {
			state, err = checklambdaState(c.AWSClients.LambdaClient(nil, nil), l.functionName)
			if err != nil {
				return false, err
			}
			if state == StateActive {
				return true, nil
			}
			time.Sleep(8 * time.Second)
			count++
		}
		return false, nil
	default:
		return false, fmt.Errorf("%s not in desired state: %s", *l.functionName, state)
	}
}

func (c *Clients) helmStatusWrapper(name *string, e *Event, functionName *string, vpc bool) (*HelmStatusData, error) {
	switch vpc {
	case true:
		r, err := invokeLambda(c.AWSClients.LambdaClient(nil, nil), functionName, e)
		if err != nil {
			return nil, err
		}
		return r.StatusData, err

	default:
		return c.HelmStatus(*name)
	}
}

func (c *Clients) helmListWrapper(e *Event, functionName *string, vpc bool) ([]HelmListData, error) {
	switch vpc {
	case true:
		r, err := invokeLambda(c.AWSClients.LambdaClient(nil, nil), functionName, e)
		if err != nil {
			return nil, err
		}
		return r.ListData, err
	default:
		return c.HelmList(e.Inputs.Config, e.Inputs.ChartDetails)
	}
}

func (c *Clients) helmInstallWrapper(e *Event, functionName *string, vpc bool) error {
	switch vpc {
	case true:
		_, err := invokeLambda(c.AWSClients.LambdaClient(nil, nil), functionName, e)
		return err
	default:
		return c.HelmInstall(e.Inputs.Config, e.Inputs.ValueOpts, e.Inputs.ChartDetails, *e.Model.ID)
	}
}

func (c *Clients) helmUpgradeWrapper(name *string, e *Event, functionName *string, vpc bool) error {
	switch vpc {
	case true:
		_, err := invokeLambda(c.AWSClients.LambdaClient(nil, nil), functionName, e)
		return err
	default:
		return c.HelmUpgrade(*name, e.Inputs.Config, e.Inputs.ValueOpts, e.Inputs.ChartDetails, *e.Model.ID)
	}
}

func (c *Clients) helmDeleteWrapper(name *string, e *Event, functionName *string, vpc bool) error {
	switch vpc {
	case true:
		_, err := invokeLambda(c.AWSClients.LambdaClient(nil, nil), functionName, e)
		return err
	default:
		return c.HelmUninstall(*name)
	}
}

func (c *Clients) kubePendingWrapper(e *Event, functionName *string, vpc bool) (bool, error) {
	switch vpc {
	case true:
		r, err := invokeLambda(c.AWSClients.LambdaClient(nil, nil), functionName, e)
		if err != nil {
			return true, err
		}
		LastKnownErrors = r.LastKnownErrors
		return r.PendingResources, err
	default:
		return c.CheckPendingResources(e.ReleaseData)
	}
}

func (c *Clients) kubeResourcesWrapper(e *Event, functionName *string, vpc bool) (map[string]interface{}, error) {
	switch vpc {
	case true:
		r, err := invokeLambda(c.AWSClients.LambdaClient(nil, nil), functionName, e)
		if err != nil {
			return nil, err
		}
		return r.Resources, err
	default:
		return c.GetKubeResources(e.ReleaseData)
	}
}
