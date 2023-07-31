package resource

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/eks"
	"github.com/aws/aws-sdk-go/service/eks/eksiface"
	"github.com/aws/aws-sdk-go/service/lambda"
	"github.com/aws/aws-sdk-go/service/sts"
	v1 "k8s.io/api/core/v1"
	rbac "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"log"
	"sigs.k8s.io/aws-iam-authenticator/pkg/token"
)

func CreateKubeClientEks(session *session.Session, svc eksiface.EKSAPI, clusterName *string) (*kubernetes.Clientset, error) {
	endpoint, token, caData, err := getEksLogin(session, svc, clusterName)
	if err != nil {
		return nil, err
	}
	return CreateKubeClientFromToken(*endpoint, *token, caData)
}

func getEksLogin(session *session.Session, svc eksiface.EKSAPI, clusterName *string) (*string, *string, []byte, error) {
	endpoint, caData, err := GetClusterDetails(svc, clusterName)
	if err != nil {
		return nil, nil, nil, err
	}

	token, err := GetToken(session, clusterName)
	if err != nil {
		return nil, nil, nil, err
	}
	return endpoint, token, caData, nil
}

func GetClusterDetails(svc eksiface.EKSAPI, clusterName *string) (*string, []byte, error) {
	// Describe cluster
	input := &eks.DescribeClusterInput{
		Name: clusterName,
	}
	result, err := svc.DescribeCluster(input)
	if err != nil {
		return nil, nil, err
	}

	// decode caData
	caData, err := base64.StdEncoding.DecodeString(*result.Cluster.CertificateAuthority.Data)
	if err != nil {
		return nil, nil, err
	}
	return result.Cluster.Endpoint, caData, nil
}

func GetToken(session *session.Session, clusterName *string) (*string, error) {
	// generate auth token
	gen, err := token.NewGenerator(false, false)
	if err != nil {
		return nil, err
	}

	tok, err := gen.GetWithOptions(&token.GetTokenOptions{
		ClusterID: *clusterName,
		Session:   session,
	})
	if err != nil {
		return nil, err
	}
	return &tok.Token, nil
}

func CreateKubeClientFromToken(endpoint string, token string, caData []byte) (*kubernetes.Clientset, error) {
	// create config
	newConfig := &rest.Config{
		Host:        endpoint,
		BearerToken: token,
		TLSClientConfig: rest.TLSClientConfig{
			CAData: caData,
		},
	}

	return kubernetes.NewForConfig(newConfig)
}

type IamAuthMap struct {
	MapUsers []userMapping
	MapRoles []roleMapping
}

type userMapping struct {
	UserArn  string   `json:"userarn,omitempty"`
	Username string   `json:"username,omitempty"`
	Groups   []string `json:"groups,omitempty"`
}

type roleMapping struct {
	RoleArn  string   `json:"rolearn,omitempty"`
	Username string   `json:"username,omitempty"`
	Groups   []string `json:"groups,omitempty"`
}

func (i IamAuthMap) GetFromCluster(clientset *kubernetes.Clientset) (*IamAuthMap, error) {
	auth, err := clientset.CoreV1().ConfigMaps("kube-system").Get(context.Background(), "aws-auth", metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	users := make([]userMapping, 0)
	roles := make([]roleMapping, 0)
	err = json.Unmarshal([]byte(auth.Data["mapUsers"]), &users)
	if err != nil {
		log.Println(err.Error())
	}
	err = json.Unmarshal([]byte(auth.Data["mapRoles"]), &roles)
	if err != nil {
		log.Println(err.Error())
	}
	i.MapUsers = users
	i.MapRoles = roles
	return &i, nil
}

func (i IamAuthMap) addCaller(sess *session.Session) (*IamAuthMap, error) {
	arn, err := getCaller(sts.New(sess))
	if err != nil {
		return nil, err
	}
	if isUserArn(arn) {
		i.MapUsers = append(i.MapUsers, userMapping{
			UserArn: *arn,
			Groups: []string{
				"aws-auth-admin",
			},
		})
	} else {
		i.MapRoles = append(i.MapRoles, roleMapping{
			RoleArn: *arn,
			Groups: []string{
				"aws-auth-admin",
			},
		})
	}
	// add role for access of private clusters in VPC
	i.MapRoles = append(i.MapRoles, roleMapping{
		RoleArn: fmt.Sprintf("arn:%s:iam::%s:role/CloudFormation-Kubernetes-VPC", *partitionFromArn(arn), *accountIdFromArn(arn)),
		Groups: []string{
			"aws-auth-admin",
		},
	})
	return &i, nil
}

func (i IamAuthMap) PushConfigMap(clientset *kubernetes.Clientset) error {
	data := map[string]string{}
	if i.MapUsers != nil {
		users, err := json.Marshal(i.MapUsers)
		if err != nil {
			return err
		}
		data["mapUsers"] = string(users)
	}
	if i.MapRoles != nil {
		roles, err := json.Marshal(i.MapRoles)
		if err != nil {
			return err
		}
		data["mapRoles"] = string(roles)
	}
	authConfigMap := &v1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "",
			APIVersion: "",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "aws-auth",
		},
		Data: data,
	}
	ctx := context.Background()
	_, err := clientset.CoreV1().ConfigMaps("kube-system").Update(ctx, authConfigMap, metav1.UpdateOptions{})
	if errors.IsNotFound(err) {
		_, err = clientset.CoreV1().ConfigMaps("kube-system").Create(ctx, authConfigMap, metav1.CreateOptions{})
	}
	if err != nil {
		return err
	}
	return nil
}

func (i IamAuthMap) addFromModel(model *Model) *IamAuthMap {
	if model == nil {
		return &i
	}
	if model.KubernetesApiAccess == nil {
		return &i
	}
	if model.KubernetesApiAccess.Users != nil {
		for _, u := range model.KubernetesApiAccess.Users {
			user := userMapping{
				UserArn: *u.Arn,
				Groups:  u.Groups,
			}
			if u.Username != nil {
				user.Username = *u.Username
			}
			i.MapUsers = append(i.MapUsers, user)
		}
	}
	if model.KubernetesApiAccess.Roles != nil {
		for _, r := range model.KubernetesApiAccess.Roles {
			role := roleMapping{
				RoleArn: *r.Arn,
				Groups:  r.Groups,
			}
			if r.Username != nil {
				role.Username = *r.Username
			}
			i.MapRoles = append(i.MapRoles, role)
		}
	}
	return &i
}

func (i IamAuthMap) removeByArn(arn *string) *IamAuthMap {
	for idx, user := range i.MapUsers {
		if user.UserArn == *arn {
			i.MapUsers = append(i.MapUsers[:idx], i.MapUsers[idx+1:]...)
		}
	}
	for idx, role := range i.MapRoles {
		if role.RoleArn == *arn {
			i.MapUsers = append(i.MapUsers[:idx], i.MapUsers[idx+1:]...)
		}
	}
	return &i
}

func putAwsAuthAdminRole(clientset *kubernetes.Clientset) error {
	role := &rbac.Role{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "aws-auth-admin",
			Namespace: "kube-system",
		},
		Rules: []rbac.PolicyRule{
			{
				Verbs:         []string{"*"},
				APIGroups:     []string{""},
				Resources:     []string{"configmaps"},
				ResourceNames: []string{"aws-auth"},
			},
		},
	}
	ctx := context.Background()
	_, err := clientset.RbacV1().Roles("kube-system").Get(ctx, "aws-auth-admin", metav1.GetOptions{})
	if !errors.IsNotFound(err) {
		_, err = clientset.RbacV1().Roles("kube-system").Create(ctx, role, metav1.CreateOptions{})
	} else {
		_, err = clientset.RbacV1().Roles("kube-system").Update(ctx, role, metav1.UpdateOptions{})
	}
	if err != nil {
		return err
	}
	roleBinding := &rbac.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "aws-auth-admin",
			Namespace: "kube-system",
		},
		Subjects: []rbac.Subject{
			{
				Kind:     "Group",
				APIGroup: "rbac.authorization.k8s.io",
				Name:     "aws-auth-admin",
			},
		},
		RoleRef: rbac.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "Role",
			Name:     "aws-auth-admin",
		},
	}
	_, err = clientset.RbacV1().RoleBindings("kube-system").Get(ctx, "aws-auth-admin", metav1.GetOptions{})
	if !errors.IsNotFound(err) {
		_, err = clientset.RbacV1().RoleBindings("kube-system").Create(ctx, roleBinding, metav1.CreateOptions{})
	} else {
		_, err = clientset.RbacV1().RoleBindings("kube-system").Update(ctx, roleBinding, metav1.UpdateOptions{})
	}
	if err != nil {
		return err
	}
	return nil
}

func createIamAuth(sess *session.Session, svc eksiface.EKSAPI, model *Model) error {
	// get kubernetes api client
	clientset, err := CreateKubeClientEks(sess, svc, model.Name)
	if err != nil {
		return err
	}
	// add Role, RoleBinding and Group
	err = putAwsAuthAdminRole(clientset)
	if err != nil {
		return err
	}
	// Add caller to authmap, so that we have permissions to perform updates to auth map.
	authMap := &IamAuthMap{}
	authMap, err = authMap.addCaller(sess)
	if err != nil {
		return err
	}

	// add iam entities from model
	authMap = authMap.addFromModel(model)

	// create aws-auth configmap
	err = authMap.PushConfigMap(clientset)
	if err != nil {
		return err
	}

	return nil
}

func updateIamAuth(sess *session.Session, svc eksiface.EKSAPI, model *Model) error {

	// Add caller to authmap, so that we have permissions to perform updates to auth map.
	authMap := &IamAuthMap{}
	authMap, err := authMap.addCaller(sess)
	if err != nil {
		return err
	}

	// add iam entities from model
	authMap = authMap.addFromModel(model)

	if isPrivate(model) {
		resp, err := invokeLambda(sess, lambda.New(sess), model.Name, authMap, UpdateAction)
		if err != nil {
			return err
		}
		log.Println(resp)
	} else {
		// get kubernetes api client
		clientset, err := CreateKubeClientEks(sess, svc, model.Name)
		if err != nil {
			return err
		}
		// create aws-auth configmap
		err = authMap.PushConfigMap(clientset)
		if err != nil {
			return err
		}
	}
	return nil
}
