package handlers

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"math/rand"
	"path/filepath"
	"strings"
	"time"

	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"

	"unisys.com/slec/filestore"
	"unisys.com/slec/utils"
)

var config *rest.Config
var resources map[string]schema.GroupVersionResource

func InitializeKubeConfig() error {
	var err error
	config, err = getInClusterConfig()
	if err != nil {
		return err
	}
	// config = localConfig()
	InitializeCloudResources()
	resources = make(map[string]schema.GroupVersionResource)
	resources["knative-services"] = schema.GroupVersionResource{
		Group:    "serving.knative.dev",
		Version:  "v1",
		Resource: "services",
	}
	resources["knative-revisions"] = schema.GroupVersionResource{
		Group:    "serving.knative.dev",
		Version:  "v1",
		Resource: "revisions",
	}
	resources["builds"] = schema.GroupVersionResource{
		Group:    "shipwright.io",
		Version:  "v1alpha1",
		Resource: "builds",
	}
	resources["buildruns"] = schema.GroupVersionResource{
		Group:    "shipwright.io",
		Version:  "v1alpha1",
		Resource: "buildruns",
	}
	resources["taskruns"] = schema.GroupVersionResource{
		Group:    "tekton.dev",
		Version:  "v1beta1",
		Resource: "taskruns",
	}
	resources["externalsecrets"] = schema.GroupVersionResource{
		Group:    "external-secrets.io",
		Version:  "v1beta1",
		Resource: "externalsecrets",
	}
	return nil
}

func GetConfig() *rest.Config {
	return config
}

func localConfig() (*rest.Config, error) {
	var kubeconfig *string
	if home := homedir.HomeDir(); home != "" {
		kubeconfig = flag.String("kubeconfig", filepath.Join(home, ".kube", "config"), "(optional) absolute path to the kubeconfig file")
	} else {
		kubeconfig = flag.String("kubeconfig", "", "absolute path to the kubeconfig file")
	}
	flag.Parse()

	config, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	if err != nil {
		return nil, err
	}
	return config, nil
}

func getInClusterConfig() (*rest.Config, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		// panic(err.Error())
		log.Println("InCluster config error. Reason - ", err.Error(), "\nTrying local kube config.")
		return localConfig()
	}
	return config, nil
}

func CreateNamespace(name string) (*apiv1.Namespace, error) {
	name = strings.ToLower(name)
	nsName := &apiv1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err)
	}

	ns, err := clientset.CoreV1().Namespaces().Create(context.Background(), nsName, metav1.CreateOptions{})
	if errors.IsAlreadyExists(err) {
		return ns, nil
	}
	return ns, err

}

func getBuildNamespace(tenant string) string {
	return tenant + "-builds"
}

func CreateTenantNamespaces(ns string) (interface{}, error) {

	_, err := CreateNamespace(ns)
	if err != nil {
		return nil, err
	}
	_, err = CreateNamespace(getBuildNamespace(ns))
	if err != nil {
		return nil, err
	}
	return nil, err
}

func CreateBuilds(tenant, appName, compName string, app Application) (interface{}, error) {
	client, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	comp := app.Components[0]

	for _, appComp := range app.Components {
		if appComp.Name == compName {
			comp = appComp
		}
	}
	// check if Credential secret is available in the namespace
	err = CheckSecretExists(getBuildNamespace(tenant), comp.SourceRepo.CredentialsKeyName)
	if err != nil {
		return nil, err
	}
	err = CheckSecretExists(getBuildNamespace(tenant), app.ContainerRegistry.CredentialsKeyName)
	if err != nil {
		return nil, err
	}

	buildStrategy := "buildpacks-v3"
	if comp.SourceRepo.UseDockerfile {
		buildStrategy = "buildah"
	}
	defn := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "shipwright.io/v1alpha1",
			"kind":       "Build",
			"metadata": map[string]interface{}{
				"name": app.Name + "-" + comp.Name,
				"labels": map[string]interface{}{
					"slec/compName": app.Name + "-" + comp.Name,
				},
				"annotations": map[string]interface{}{
					"build.shipwright.io/verify.repository": "true",
				},
			},
			"spec": map[string]interface{}{
				// "buildSpec": map[string]interface{}{
				"source": map[string]interface{}{
					"url":        comp.SourceRepo.URL,
					"contextDir": comp.SourceRepo.ContextDir,
					"revision":   comp.SourceRepo.Revision,
					"credentials": map[string]interface{}{
						"name": comp.SourceRepo.CredentialsKeyName,
					},
				},
				"strategy": map[string]interface{}{
					"name": buildStrategy,
					"kind": "ClusterBuildStrategy",
				},
				"output": map[string]interface{}{
					"image": app.ContainerRegistry.URL + "-" + comp.Name,
					"credentials": map[string]interface{}{
						"name": app.ContainerRegistry.CredentialsKeyName,
					},
				},
				// "retention": map[string]interface{}{
				// 	"ttlAfterFailed":    "45m",
				// 	"ttlAfterSucceeded": "1h",
				// 	"failedLimit":       5,
				// 	"succeededLimit":    5,
				// },
				// },
			},
		},
	}
	return client.
		Resource(resources["builds"]).
		Namespace(getBuildNamespace(tenant)). //  apiv1.NamespaceDefault
		Apply(context.TODO(), app.Name+"-"+comp.Name, defn, metav1.ApplyOptions{FieldManager: app.Name + "-" + comp.Name})
	// Create(context.TODO(), defn, metav1.CreateOptions{})
}

func FindAndUpdateCompProgress(appRequest *Application, compName string, tenant string, stage int, result int, who string) {

	tempapp, err := filestore.GetRecord(tenant, "apps", appRequest.Name)
	if err != nil {
		log.Println("error  Name: ", appRequest.Name)
	}
	// create an Application{} object as we need to update states and times
	appJson, _ := json.Marshal(tempapp)
	var application Application
	err = json.Unmarshal(appJson, &application)
	if err != nil {
		log.Println("error  Name: ", compName)
	}
	compIndex := getCompIndex(&application, compName)
	if stage == 1 { // build
		if result == 0 { // failed build OR timeout
			application.Components[compIndex].States.Current_state = "Provisioned"
			application.Components[compIndex].States.Provisioned.Status = true
			application.Components[compIndex].States.Built.Status = false
			application.Components[compIndex].States.Built.Progress = 0
			sendNotification(appRequest.Name, compName, "build failed.")

		} else { // successful build
			application.Components[compIndex].States.Current_state = "Built"
			application.Components[compIndex].States.Built.Status = true
			application.Components[compIndex].States.Built.When = time.Now()
			application.Components[compIndex].States.Built.Progress = 100
			sendNotification(appRequest.Name, compName, "build successful.")
		}
	} else if stage == 2 { // deployment
		if result == 0 { // failed deployment OR timeout
			application.Components[compIndex].States.Current_state = "Built"
			application.Components[compIndex].States.Deployed.Status = false
			application.Components[compIndex].States.Deployed.Progress = 0
			application.Components[compIndex].States.Built.Status = true
			sendNotification(appRequest.Name, compName, "deployment failed.")
		} else { // successful deployment
			application.Components[compIndex].States.Current_state = "Deployed"
			application.Components[compIndex].States.Deployed.Status = true
			application.Components[compIndex].States.Deployed.Progress = 100
			application.Components[compIndex].States.Deployed.When = time.Now()
			sendNotification(appRequest.Name, compName, "deployment successful.")
		}

	}
	ForceWorkloadUpdate(tenant, &application, who)
	b, _ := json.Marshal(application)
	var r map[string]any
	json.Unmarshal(b, &r)
	sendDataUsingSSE(&r)
	
}

func ForceWorkloadUpdate(tenant string, updatedApp *Application, who string) {

	byteArr, err := json.Marshal(updatedApp)
	if err != nil {
		log.Println("Error while Marshalling app data")
		return
	}
	content := make(map[string]any)
	json.Unmarshal(byteArr, &content)

	err = filestore.UpdateRecord(tenant, "apps", updatedApp.Name, "", content, who)
	if err != nil {
		log.Println("Error while update application record")
		return
	}
}

func getCompIndex(app *Application, compname string) int {
	var compIndex int
	for index, appComp := range app.Components {
		if appComp.Name == compname {
			compIndex = index
			break
		}
	}
	return compIndex
}

func CheckBuildStatusInInterval(app *Application, compname string, buildName string, tenant string, who string) {

buildCheckLoop:
	for timeout := time.After(time.Second * 300); ; {
		select {
		case <-timeout:
			log.Println("Build Operaton timed out - update status")
			sendNotification(app.Name, compname, "build timed out.")
			FindAndUpdateCompProgress(app, compname, tenant, 1, 0, who)
			break buildCheckLoop
		default:
			time.Sleep(time.Second * 20)
			status, err := checkIfBuildCompleted(app, compname, buildName, tenant)
			if err == nil && status == 2 {
				FindAndUpdateCompProgress(app, compname, tenant, 1, 0, who)
				break buildCheckLoop
			}
			if err == nil {
				if status == 0 {
					// do nothing
				} else if status == 1 { // build successful
					FindAndUpdateCompProgress(app, compname, tenant, 1, 1, who)
					break buildCheckLoop
				} // else status

			}
		}

	}

}

func sendNotification(appName string, compName string, statusMessage string) {
	var r map[string]any

	notify := Notification{}
	notify.AppName = appName
	notify.CompName = compName
	notify.StatusMessage = statusMessage
	notify.NotifyId = rand.Intn(1000)
	notify.NotifyTime = time.Now()

	b, _ := json.Marshal(notify)

	json.Unmarshal(b, &r)
	sendDataUsingSSE(&r)

}

func checkIfBuildCompleted(app *Application, compname string, buildNameToSearch string, tenant string) (int, error) {
	buildName := app.Name + "-" + compname
	res, err := GetBuildRuns(tenant, buildName)
	if err != nil {
		//comp.States.Built.Status = false
		return 0, err
	}
	b, _ := json.Marshal(res)
	var latestTask string
	var payload map[string]interface{}
	var buildTypeValue string
	var buildStatusValue string
	err = json.Unmarshal(b, &payload)
	mapstringinterface := payload["items"].([]interface{})

	for mapindex, _ := range mapstringinterface {
		buildNameInMap := payload["items"].([]interface{})[mapindex].(map[string]interface{})["metadata"].(map[string]interface{})["name"].(string)
		if buildNameInMap == buildNameToSearch {
			buildTypeValue = mapstringinterface[mapindex].(map[string]interface{})["status"].(map[string]interface{})["conditions"].([]interface{})[0].(map[string]interface{})["type"].(string)
			buildStatusValue = mapstringinterface[mapindex].(map[string]interface{})["status"].(map[string]interface{})["conditions"].([]interface{})[0].(map[string]interface{})["status"].(string)
			latestTask = mapstringinterface[mapindex].(map[string]interface{})["status"].(map[string]interface{})["latestTaskRunRef"].(string)
			break
		} //else {
		//log.Println("Continue looking for buildname in build runs")
		//}

	}
	if buildTypeValue == "Succeeded" && buildStatusValue == "True" {
		log.Println("Successful build - update status")
		res, _ := GetTaskRun(tenant, latestTask)
		b, _ := json.Marshal(res)
		var r map[string]any
		json.Unmarshal(b, &r)
		sendDataUsingSSE(&r)
		
		return 1, nil
	}

	if buildTypeValue == "Succeeded" && buildStatusValue == "False" {
		log.Println("Failed build - update status")
		return 2, nil
	}
	return 0, err

}
func InitiateBuildRun(tenant, appName, compName, version string, app Application) (interface{}, error) {
	client, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	comp := app.Components[0]
	for _, appComp := range app.Components {
		if appComp.Name == compName {
			comp = appComp
		}
	}

	sendNotification(appName, compName, "build initiated.")

	defn := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "shipwright.io/v1alpha1",
			"kind":       "BuildRun",
			"metadata": map[string]interface{}{
				"labels": map[string]interface{}{
					"slec/compName": app.Name + "-" + comp.Name,
				},
				"generateName": app.Name + "-" + comp.Name + "-",
			},
			"spec": map[string]interface{}{
				"buildRef": map[string]interface{}{
					"name": app.Name + "-" + comp.Name,
				},
				"output": map[string]interface{}{
					"image": app.ContainerRegistry.URL + "-" + comp.Name + ":" + version,
					"credentials": map[string]interface{}{
						"name": app.ContainerRegistry.CredentialsKeyName,
					},
				},
			},
		},
	}
	return client.
		Resource(resources["buildruns"]).
		Namespace(getBuildNamespace(tenant)). //  apiv1.NamespaceDefault
		// Update(context.TODO(), defn, metav1.UpdateOptions{})
		// Apply(context.TODO(), app.Name+"-"+comp.Name, defn, metav1.ApplyOptions{FieldManager: ".spec.output.image"})
		Create(context.TODO(), defn, metav1.CreateOptions{})
}

func GetKnativeServices(tenant, appName, compName, env string) (interface{}, error) {
	ns := tenant + "-" + env + "-" + appName
	lbl := map[string]string{"slec/compName": compName}
	return listHelper(ns, labels.SelectorFromSet(lbl).String(), "knative-services")
}

func GetKnativeRevisions(tenant, appName, compName, env string) (interface{}, error) {
	ns := tenant + "-" + env + "-" + appName
	lbl := map[string]string{"serving.knative.dev/service": compName}
	return listHelper(ns, labels.SelectorFromSet(lbl).String(), "knative-revisions")
}

func GetBuildRuns(tenant, buildName string) (interface{}, error) {
	lbl := map[string]string{"build.shipwright.io/name": buildName}
	return listHelper(getBuildNamespace(tenant), labels.SelectorFromSet(lbl).String(), "buildruns")
}

func GetTaskRuns(tenant, buildName string) (interface{}, error) {
	lbl := map[string]string{"build.shipwright.io/name": buildName}
	return listHelper(getBuildNamespace(tenant), labels.SelectorFromSet(lbl).String(), "taskruns")
}

func GetTaskRun(tenant, name string) (interface{}, error) {
	return getHelper(getBuildNamespace(tenant), name, "taskruns")
}

func CheckBuildTaskCompletion(tenant, name string) {
	ticker := time.NewTicker(10 * time.Second)
	quit := make(chan struct{})
	go func() {
		for {
			select {
			case <-ticker.C:
				res, err := GetTaskRun(tenant, name)
				if err != nil {
					close(quit)
				}
				b, _ := json.Marshal(res)
				var r map[string]any
				json.Unmarshal(b, &r)
				reason, err := utils.GetJsonPathValue(r, "status.conditions.0.reason")
				if reason.(string) != "Running" {
					log.Println(tenant, name, "Task completed. invoke sse.")
					//sendDataUsingSSE(&r)
					close(quit)
				}
				// return
			case <-quit:
				ticker.Stop()
				return
			}
		}
	}()
}

func listHelper(namespace, lblSelector, resourceType string) (interface{}, error) {
	client, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	return client.
		Resource(resources[resourceType]).
		Namespace(namespace).
		List(context.Background(), metav1.ListOptions{
			LabelSelector: lblSelector,
		})
}

func getHelper(namespace, name, resourceType string) (interface{}, error) {
	client, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	return client.
		Resource(resources[resourceType]).
		Namespace(namespace).
		Get(context.Background(), name, metav1.GetOptions{})
}

func GetLogs(tenant, podName, containerName string) ([]byte, error) {
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err)
	}
	opts := apiv1.PodLogOptions{
		Container: containerName,
	}
	return clientset.CoreV1().
		Pods(getBuildNamespace(tenant)).
		GetLogs(podName, &opts).
		Do(context.Background()).Raw()
}

func copySecrets(secretName, nsFrom, nsTo string, secretToName string) error {
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return err
	}

	secret, err := clientset.CoreV1().Secrets(nsFrom).Get(context.Background(), secretName, metav1.GetOptions{})
	if err != nil {
		return err
	}

	newSecret := secret.DeepCopy()
	newSecret.ObjectMeta.Namespace = nsTo
	newSecret.ResourceVersion = ""
	if secretToName != "" {
		newSecret.Name = secretToName
	}

	_, err = clientset.CoreV1().Secrets(nsTo).Create(context.Background(), newSecret, metav1.CreateOptions{})
	if err != nil {
		return err
	}
	return nil
}
func CreateExternalSecretsSourceCodeRepository(ns string, key string, targetName string) error {

	client, err := dynamic.NewForConfig(config)
	if err != nil {
		return err
	}

	defn := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "external-secrets.io/v1beta1",
			"kind":       "ExternalSecret",
			"metadata": map[string]interface{}{
				"name":      targetName,
				"namespace": ns,
			},
			"spec": map[string]interface{}{
				"data": []map[string]interface{}{
					{
						"remoteRef": map[string]interface{}{
							"conversionStrategy": "Default",
							"decodingStrategy":   "None",
							"key":                key,
							"property":           "username",
						},
						"secretKey": "username",
					},
					{
						"remoteRef": map[string]interface{}{
							"conversionStrategy": "Default",
							"decodingStrategy":   "None",
							"key":                key,
							"property":           "password",
						},
						"secretKey": "password",
					},
				},
				"refreshInterval": "15s",
				"secretStoreRef": map[string]interface{}{
					"kind": "ClusterSecretStore",
					"name": "vault-backend",
				},
				"target": map[string]interface{}{
					"creationPolicy": "Owner",
					"deletionPolicy": "Retain",
					"name":           targetName,
				},
			},
		},
	}
	_, err = client.
		Resource(resources["externalsecrets"]).
		Namespace(ns).
		Apply(context.TODO(), targetName, defn, metav1.ApplyOptions{Force: true, FieldManager: targetName})

	if errors.IsAlreadyExists(err) {
		return nil
	}
	return err
}
func CreateExternalSecretsContainerRegistry(ns string, key string, targetName string) error {
	client, err := dynamic.NewForConfig(config)
	if err != nil {
		return err
	}

	defn := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "external-secrets.io/v1beta1",
			"kind":       "ExternalSecret",
			"metadata": map[string]interface{}{
				"name":      targetName,
				"namespace": ns,
			},
			"spec": map[string]interface{}{
				"refreshInterval": "15s",
				"secretStoreRef": map[string]interface{}{
					"name": "vault-backend",
					"kind": "ClusterSecretStore",
				},
				"target": map[string]interface{}{
					"template": map[string]interface{}{
						"type": "kubernetes.io/dockerconfigjson",
						"data": map[string]interface{}{
							".dockerconfigjson": "{{ .dockerconfigjson | toString }}",
						},
					},
					"name":           targetName,
					"creationPolicy": "Owner",
				},
				"data": []map[string]interface{}{
					{
						"secretKey": "dockerconfigjson",
						"remoteRef": map[string]interface{}{
							"key": key,
						},
					},
				},
			},
		},
	}
	_, err = client.
		Resource(resources["externalsecrets"]).
		Namespace(ns).
		Apply(context.TODO(), targetName, defn, metav1.ApplyOptions{Force: true, FieldManager: targetName})

	if errors.IsAlreadyExists(err) {
		return nil
	}
	return err
}
func CheckSecretExists(ns string, credential string) error {
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err)
	}

	_, err = clientset.CoreV1().Secrets(ns).Get(context.TODO(), credential, metav1.GetOptions{})
	return err

}
