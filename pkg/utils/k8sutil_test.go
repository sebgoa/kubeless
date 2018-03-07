package utils

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"testing"

	kubelessApi "github.com/kubeless/kubeless/pkg/apis/kubeless/v1beta1"
	"github.com/kubeless/kubeless/pkg/langruntime"

	v2beta1 "k8s.io/api/autoscaling/v2beta1"
	batchv2alpha1 "k8s.io/api/batch/v2alpha1"
	"k8s.io/api/core/v1"
	"k8s.io/api/extensions/v1beta1"
	xv1beta1 "k8s.io/api/extensions/v1beta1"
	k8sErrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apimachinery"
	"k8s.io/apimachinery/pkg/apimachinery/registered"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	restFake "k8s.io/client-go/rest/fake"
	ktesting "k8s.io/client-go/testing"
)

func getEnvValueFromList(envName string, l []v1.EnvVar) string {
	var res v1.EnvVar
	for _, env := range l {
		if env.Name == envName {
			res = env
			break
		}
	}
	return res.Value
}

func TestEnsureConfigMap(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	or := []metav1.OwnerReference{
		{
			Kind:       "Function",
			APIVersion: "k8s.io",
		},
	}
	ns := "default"
	funcLabels := map[string]string{
		"foo": "bar",
	}
	f1Name := "f1"
	f1 := &kubelessApi.Function{
		ObjectMeta: metav1.ObjectMeta{
			Name:      f1Name,
			Namespace: ns,
			Labels:    funcLabels,
		},
		Spec: kubelessApi.FunctionSpec{
			Function: "function",
			Deps:     "deps",
			Handler:  "foo.bar",
			Runtime:  "python2.7",
		},
	}

	langruntime.AddFakeConfig(clientset)
	lr := langruntime.SetupLangRuntime(clientset)
	lr.ReadConfigMap()

	err := EnsureFuncConfigMap(clientset, f1, or, lr)
	if err != nil {
		t.Errorf("Unexpected error: %s", err)
	}
	cm, err := clientset.CoreV1().ConfigMaps(ns).Get(f1Name, metav1.GetOptions{})
	if err != nil {
		t.Errorf("Unexpected error: %s", err)
	}
	expectedCM := v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:            f1Name,
			Namespace:       ns,
			Labels:          funcLabels,
			OwnerReferences: or,
		},
		Data: map[string]string{
			"handler":          "foo.bar",
			"foo.py":           "function",
			"requirements.txt": "deps",
		},
	}
	if !reflect.DeepEqual(*cm, expectedCM) {
		t.Errorf("Unexpected ConfigMap:\n %+v\nExpecting:\n %+v", *cm, expectedCM)
	}

	// It should skip the dependencies field in case it is not supported
	f2Name := "f2"
	f2 := &kubelessApi.Function{
		ObjectMeta: metav1.ObjectMeta{
			Name:      f2Name,
			Namespace: ns,
		},
		Spec: kubelessApi.FunctionSpec{
			Function: "function",
			Handler:  "foo.bar",
			Runtime:  "cobol",
		},
	}

	err = EnsureFuncConfigMap(clientset, f2, or, lr)
	if err != nil {
		t.Errorf("Unexpected error: %s", err)
	}
	cm, err = clientset.CoreV1().ConfigMaps(ns).Get(f2Name, metav1.GetOptions{})
	if err != nil {
		t.Errorf("Unexpected error: %s", err)
	}
	expectedData := map[string]string{
		"handler": "foo.bar",
		"foo":     "function",
	}
	if !reflect.DeepEqual(cm.Data, expectedData) {
		t.Errorf("Unexpected ConfigMap:\n %+v\nExpecting:\n %+v", cm.Data, expectedData)
	}

	// If there is already a config map it should update the previous one
	f2 = &kubelessApi.Function{
		ObjectMeta: metav1.ObjectMeta{
			Name:      f2Name,
			Namespace: ns,
		},
		Spec: kubelessApi.FunctionSpec{
			Function: "function2",
			Handler:  "foo2.bar2",
			Runtime:  "python3.4",
		},
	}
	err = EnsureFuncConfigMap(clientset, f2, or, lr)
	if err != nil {
		t.Errorf("Unexpected error: %s", err)
	}
	cm, err = clientset.CoreV1().ConfigMaps(ns).Get(f2Name, metav1.GetOptions{})
	if err != nil {
		t.Errorf("Unexpected error: %s", err)
	}
	expectedData = map[string]string{
		"handler":          "foo2.bar2",
		"foo2.py":          "function2",
		"requirements.txt": "",
	}
	if !reflect.DeepEqual(cm.Data, expectedData) {
		t.Errorf("Unexpected ConfigMap:\n %+v\nExpecting:\n %+v", cm.Data, expectedData)
	}
}

func TestEnsureService(t *testing.T) {
	fakeSvc := v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "myns",
			Name:      "foo",
		},
	}
	clientset := fake.NewSimpleClientset(&fakeSvc)
	or := []metav1.OwnerReference{
		{
			Kind:       "Function",
			APIVersion: "k8s.io",
		},
	}
	ns := "default"
	funcLabels := map[string]string{
		"foo": "bar",
	}
	f1Name := "f1"
	f1 := &kubelessApi.Function{
		ObjectMeta: metav1.ObjectMeta{
			Name:      f1Name,
			Namespace: ns,
			Labels:    funcLabels,
		},
		Spec: kubelessApi.FunctionSpec{
			Function: "function",
			Deps:     "deps",
			Handler:  "foo.bar",
			Runtime:  "python2.7",
		},
	}
	err := EnsureFuncService(clientset, f1, or)
	if err != nil {
		t.Errorf("Unexpected error: %s", err)
	}
	svc, err := clientset.CoreV1().Services(ns).Get(f1Name, metav1.GetOptions{})
	if err != nil {
		t.Errorf("Unexpected error: %s", err)
	}
	expectedSVC := v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:            f1Name,
			Namespace:       ns,
			Labels:          funcLabels,
			OwnerReferences: or,
		},
		Spec: v1.ServiceSpec{
			Ports: []v1.ServicePort{
				{
					Name:       "http-function-port",
					Port:       8080,
					TargetPort: intstr.FromInt(8080),
					NodePort:   0,
					Protocol:   v1.ProtocolTCP,
				},
			},
			Selector: funcLabels,
			Type:     v1.ServiceTypeClusterIP,
		},
	}
	if !reflect.DeepEqual(*svc, expectedSVC) {
		t.Errorf("Unexpected service:\n %+v\nExpecting:\n %+v", *svc, expectedSVC)
	}

	// If there is already a service it should update the previous one
	newLabels := map[string]string{
		"foobar": "barfoo",
	}
	f1.ObjectMeta.Labels = newLabels
	err = EnsureFuncService(clientset, f1, or)
	if err != nil {
		t.Errorf("Unexpected error: %s", err)
	}
	svc, err = clientset.CoreV1().Services(ns).Get(f1Name, metav1.GetOptions{})
	if err != nil {
		t.Errorf("Unexpected error: %s", err)
	}
	if !reflect.DeepEqual(svc.ObjectMeta.Labels, newLabels) {
		t.Error("Unable to update the service")
	}
}

func TestEnsureDeployment(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	or := []metav1.OwnerReference{
		{
			Kind:       "Function",
			APIVersion: "k8s.io",
		},
	}
	ns := "default"
	funcLabels := map[string]string{
		"foo": "bar",
	}
	funcAnno := map[string]string{
		"bar": "foo",
	}

	langruntime.AddFakeConfig(clientset)
	lr := langruntime.SetupLangRuntime(clientset)
	lr.ReadConfigMap()

	f1Name := "f1"
	f1Port := int32(8080)
	f1 := &kubelessApi.Function{
		ObjectMeta: metav1.ObjectMeta{
			Name:      f1Name,
			Namespace: ns,
			Labels:    funcLabels,
		},
		Spec: kubelessApi.FunctionSpec{
			Function: "function",
			Deps:     "deps",
			Handler:  "foo.bar",
			Runtime:  "python2.7",
			Deployment: v1beta1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: funcAnno,
				},
				Spec: v1beta1.DeploymentSpec{
					Template: v1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Annotations: funcAnno,
						},
						Spec: v1.PodSpec{
							Containers: []v1.Container{
								{
									Env: []v1.EnvVar{
										{
											Name:  "foo",
											Value: "bar",
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
	// Testing happy path
	err := EnsureFuncDeployment(clientset, f1, or, lr)
	if err != nil {
		t.Errorf("Unexpected error: %s", err)
	}
	dpm, err := clientset.ExtensionsV1beta1().Deployments(ns).Get(f1Name, metav1.GetOptions{})
	if err != nil {
		t.Errorf("Unexpected error: %s", err)
	}
	expectedObjectMeta := metav1.ObjectMeta{
		Name:            f1Name,
		Namespace:       ns,
		Labels:          funcLabels,
		OwnerReferences: or,
		Annotations:     funcAnno,
	}
	if !reflect.DeepEqual(dpm.ObjectMeta, expectedObjectMeta) {
		t.Errorf("Unable to set metadata. Received:\n %+v\nExpecting:\n %+v", dpm.ObjectMeta, expectedObjectMeta)
	}
	expectedAnnotations := map[string]string{
		"prometheus.io/scrape": "true",
		"prometheus.io/path":   "/metrics",
		"prometheus.io/port":   strconv.Itoa(int(f1Port)),
		"bar":                  "foo",
	}
	for i := range expectedAnnotations {
		if dpm.Spec.Template.Annotations[i] != expectedAnnotations[i] {
			t.Errorf("Expecting annotation %s but received %s", expectedAnnotations[i], dpm.Spec.Template.Annotations[i])
		}
	}
	if dpm.Spec.Template.Annotations["bar"] != "foo" {
		t.Error("Unable to set annotations")
	}
	expectedContainer := v1.Container{
		Name:  f1Name,
		Image: "kubeless/python@sha256:0f3b64b654df5326198e481cd26e73ecccd905aae60810fc9baea4dcbb61f697",
		Ports: []v1.ContainerPort{
			{
				ContainerPort: int32(f1Port),
			},
		},
		Env: []v1.EnvVar{
			{
				Name:  "foo",
				Value: "bar",
			},
			{
				Name:  "FUNC_HANDLER",
				Value: "bar",
			},
			{
				Name:  "MOD_NAME",
				Value: "foo",
			},
			{
				Name:  "FUNC_TIMEOUT",
				Value: "180",
			},
			{
				Name:  "FUNC_RUNTIME",
				Value: "python2.7",
			},
			{
				Name:  "FUNC_MEMORY_LIMIT",
				Value: "0",
			},
			{
				Name:  "FUNC_PORT",
				Value: strconv.Itoa(int(f1Port)),
			},
			{
				Name:  "PYTHONPATH",
				Value: "/kubeless/lib/python2.7/site-packages",
			},
		},
		VolumeMounts: []v1.VolumeMount{
			{
				Name:      f1Name,
				MountPath: "/kubeless",
			},
		},
		LivenessProbe: &v1.Probe{
			InitialDelaySeconds: int32(3),
			PeriodSeconds:       int32(30),
			Handler: v1.Handler{
				HTTPGet: &v1.HTTPGetAction{
					Path: "/healthz",
					Port: intstr.FromInt(int(f1Port)),
				},
			},
		},
	}
	if !reflect.DeepEqual(dpm.Spec.Template.Spec.Containers[0], expectedContainer) {
		t.Errorf("Unexpected container definition. Received:\n %+v\nExpecting:\n %+v", dpm.Spec.Template.Spec.Containers[0], expectedContainer)
	}

	secrets := dpm.Spec.Template.Spec.ImagePullSecrets
	if secrets[0].Name != "p1" && secrets[1].Name != "p2" {
		t.Errorf("Expected first secret to be 'p1' but found %v and second secret to be 'p2' and found %v", secrets[0], secrets[1])
	}

	// Init containers behavior should be tested with integration tests
	if len(dpm.Spec.Template.Spec.InitContainers) < 1 {
		t.Errorf("Expecting at least an init container to install deps")
	}

	// If no handler and function is given it should not fail
	f2 := kubelessApi.Function{}
	f2 = *f1
	f2.ObjectMeta.Name = "func2"
	f2.Spec.Function = ""
	f2.Spec.Handler = ""
	err = EnsureFuncDeployment(clientset, &f2, or, lr)
	if err != nil {
		t.Errorf("Unexpected error: %s", err)
	}
	dpm, err = clientset.ExtensionsV1beta1().Deployments(ns).Get("func2", metav1.GetOptions{})
	if err != nil {
		t.Errorf("Unexpected error: %s", err)
	}

	// If the Image has been already provided it should not resolve it
	f3 := kubelessApi.Function{}
	f3 = *f1
	f3.ObjectMeta.Name = "func3"
	f3.Spec.Deployment.Spec.Template.Spec.Containers[0].Image = "test-image"
	err = EnsureFuncDeployment(clientset, &f3, or, lr)
	if err != nil {
		t.Errorf("Unexpected error: %s", err)
	}
	dpm, err = clientset.ExtensionsV1beta1().Deployments(ns).Get("func3", metav1.GetOptions{})
	if err != nil {
		t.Errorf("Unexpected error: %s", err)
	}
	if dpm.Spec.Template.Spec.Containers[0].Image != "test-image" {
		t.Errorf("Unexpected Image Name: %s", dpm.Spec.Template.Spec.Containers[0].Image)
	}

	// If no function is given it should not use an init container
	f4 := kubelessApi.Function{}
	f4 = *f1
	f4.ObjectMeta.Name = "func4"
	f4.Spec.Function = ""
	f4.Spec.Deps = ""
	err = EnsureFuncDeployment(clientset, &f4, or, lr)
	if err != nil {
		t.Errorf("Unexpected error: %s", err)
	}
	dpm, err = clientset.ExtensionsV1beta1().Deployments(ns).Get("func4", metav1.GetOptions{})
	if err != nil {
		t.Errorf("Unexpected error: %s", err)
	}
	if len(dpm.Spec.Template.Spec.InitContainers) > 0 {
		t.Error("It should not setup an init container")
	}

	// It should update a deployment if it is already present
	f6 := kubelessApi.Function{}
	f6 = *f1
	f6.Spec.Handler = "foo.bar2"
	f6.Spec.Deployment.ObjectMeta.Annotations["new-key"] = "value"
	err = EnsureFuncDeployment(clientset, &f6, or, lr)
	if err != nil {
		t.Errorf("Unexpected error: %s", err)
	}
	dpm, err = clientset.ExtensionsV1beta1().Deployments(ns).Get(f1Name, metav1.GetOptions{})
	if err != nil {
		t.Errorf("Unexpected error: %s", err)
	}
	if getEnvValueFromList("FUNC_HANDLER", dpm.Spec.Template.Spec.Containers[0].Env) != "bar2" {
		t.Error("Unable to update deployment")
	}
	if dpm.Annotations["new-key"] != "value" {
		t.Errorf("Unable to update deployment %v", dpm.Annotations)
	}

	// It should return an error if some dependencies are given but the runtime is not supported
	f7 := kubelessApi.Function{}
	f7 = *f1
	f7.ObjectMeta.Name = "func7"
	f7.Spec.Deps = "deps"
	f7.Spec.Runtime = "cobol"
	err = EnsureFuncDeployment(clientset, &f7, or, lr)

	if err == nil {
		t.Errorf("An error should be thrown")
	}

	// If a timeout is specified it should set an environment variable FUNC_TIMEOUT
	f8 := kubelessApi.Function{}
	f8 = *f1
	f8.ObjectMeta.Name = "func8"
	f8.Spec.Timeout = "10"
	err = EnsureFuncDeployment(clientset, &f8, or, lr)
	if err != nil {
		t.Errorf("Unexpected error: %s", err)
	}
	dpm, err = clientset.ExtensionsV1beta1().Deployments(ns).Get("func8", metav1.GetOptions{})
	if err != nil {
		t.Errorf("Unexpected error: %s", err)
	}
	if getEnvValueFromList("FUNC_TIMEOUT", dpm.Spec.Template.Spec.Containers[0].Env) != "10" {
		t.Error("Unable to set timeout")
	}
}

func fakeRESTClient(f func(req *http.Request) (*http.Response, error)) *restFake.RESTClient {
	reg := registered.NewOrDie("v1")
	legacySchema := schema.GroupVersion{
		Group:   "",
		Version: "v1",
	}
	newSchema := schema.GroupVersion{
		Group:   "k8s.io",
		Version: "v1",
	}
	reg.RegisterGroup(apimachinery.GroupMeta{
		GroupVersion: legacySchema,
	})
	reg.RegisterGroup(apimachinery.GroupMeta{
		GroupVersion: newSchema,
	})
	return &restFake.RESTClient{
		APIRegistry:          reg,
		NegotiatedSerializer: scheme.Codecs,
		Client:               restFake.CreateHTTPClient(f),
	}
}

func objBody(object interface{}) io.ReadCloser {
	output, err := json.Marshal(object)
	if err != nil {
		panic(err)
	}
	return ioutil.NopCloser(bytes.NewReader([]byte(output)))
}

func TestEnsureCronJob(t *testing.T) {
	or := []metav1.OwnerReference{
		{
			Kind:       "Function",
			APIVersion: "k8s.io",
		},
	}
	ns := "default"
	f1Name := "func1"
	f1 := &kubelessApi.Function{
		ObjectMeta: metav1.ObjectMeta{
			Name:      f1Name,
			Namespace: ns,
		},
		Spec: kubelessApi.FunctionSpec{
			Timeout: "120",
		},
	}
	c := &kubelessApi.CronJobTrigger{
		ObjectMeta: metav1.ObjectMeta{
			Name:      f1Name,
			Namespace: ns,
		},
		Spec: kubelessApi.CronJobTriggerSpec{
			Schedule:     "*/10 * * * *",
			FunctionName: f1Name,
		},
	}
	expectedMeta := metav1.ObjectMeta{
		Name:            "trigger-" + f1Name,
		Namespace:       ns,
		OwnerReferences: or,
	}

	client := fakeRESTClient(func(req *http.Request) (*http.Response, error) {
		header := http.Header{}
		header.Set("Content-Type", runtime.ContentTypeJSON)
		listObj := batchv2alpha1.CronJobList{}
		if req.Method == "POST" {
			reqCronJobBytes, err := ioutil.ReadAll(req.Body)
			if err != nil {
				t.Fatal(err)
			}
			cronJob := batchv2alpha1.CronJob{}
			err = json.Unmarshal(reqCronJobBytes, &cronJob)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(expectedMeta, cronJob.ObjectMeta) {
				t.Errorf("Unexpected metadata metadata. Expecting\n%+v \nReceived:\n%+v", expectedMeta, cronJob.ObjectMeta)
			}
			if *cronJob.Spec.SuccessfulJobsHistoryLimit != int32(3) {
				t.Errorf("Unexpected SuccessfulJobsHistoryLimit: %d", *cronJob.Spec.SuccessfulJobsHistoryLimit)
			}
			if *cronJob.Spec.FailedJobsHistoryLimit != int32(1) {
				t.Errorf("Unexpected FailedJobsHistoryLimit: %d", *cronJob.Spec.FailedJobsHistoryLimit)
			}
			if *cronJob.Spec.JobTemplate.Spec.ActiveDeadlineSeconds != int64(120) {
				t.Errorf("Unexpected ActiveDeadlineSeconds: %d", *cronJob.Spec.JobTemplate.Spec.ActiveDeadlineSeconds)
			}
			expectedCommand := []string{"curl", "-Lv", fmt.Sprintf("http://%s.%s.svc.cluster.local:8080", f1Name, ns)}
			args := cronJob.Spec.JobTemplate.Spec.Template.Spec.Containers[0].Args
			// skip event headers data (i.e  -H "event-id: cronjob-controller-2018-03-05T05:55:41.990784027Z" etc)
			foundCommand := []string{args[0], args[1], args[len(args)-1]}
			if !reflect.DeepEqual(foundCommand, expectedCommand) {
				t.Errorf("Unexpected command %s expexted %s", foundCommand, expectedCommand)
			}
		} else {
			t.Fatalf("unexpected verb %s", req.Method)
		}
		switch req.URL.Path {
		case "/apis/batch/v2alpha1/namespaces/default/cronjobs":
			return &http.Response{
				StatusCode: 200,
				Header:     header,
				Body:       objBody(&listObj),
			}, nil
		default:
			t.Fatalf("unexpected request: %#v\n%#v", req.URL, req)
			return nil, nil
		}
	})
	err := EnsureCronJob(client, f1, c, or, "batch/v2alpha1")
	if err != nil {
		t.Errorf("Unexpected error: %s", err)
	}

	// It should update the existing cronJob if it is already created
	updateCalled := false
	client = fakeRESTClient(func(req *http.Request) (*http.Response, error) {
		header := http.Header{}
		header.Set("Content-Type", runtime.ContentTypeJSON)
		switch req.Method {
		case "POST":
			return &http.Response{
				StatusCode: http.StatusConflict,
				Header:     header,
				Body:       objBody(nil),
			}, nil
		case "GET":
			previousCronJob := batchv2alpha1.CronJob{
				ObjectMeta: metav1.ObjectMeta{
					ResourceVersion: "123456",
				},
			}
			return &http.Response{
				StatusCode: 200,
				Header:     header,
				Body:       objBody(&previousCronJob),
			}, nil
		case "PUT":
			updateCalled = true
			reqCronJobBytes, err := ioutil.ReadAll(req.Body)
			if err != nil {
				t.Fatal(err)
			}
			cronJob := batchv2alpha1.CronJob{}
			err = json.Unmarshal(reqCronJobBytes, &cronJob)
			if err != nil {
				t.Fatal(err)
			}
			if cronJob.ObjectMeta.ResourceVersion != "123456" {
				t.Error("Expecting that the object to update contains the previous information")
			}
			listObj := batchv2alpha1.CronJobList{}
			return &http.Response{
				StatusCode: 200,
				Header:     header,
				Body:       objBody(&listObj),
			}, nil
		default:
			t.Fatalf("unexpected request: %#v\n%#v", req.URL, req)
			return nil, nil
		}
	})
	err = EnsureCronJob(client, f1, c, or, "batch/v2alpha1")
	if err != nil {
		t.Errorf("Unexpected error: %s", err)
	}
	if !updateCalled {
		t.Errorf("Expect the update method to be called")
	}

	// IT should change the endpoint
	client = fakeRESTClient(func(req *http.Request) (*http.Response, error) {
		header := http.Header{}
		header.Set("Content-Type", runtime.ContentTypeJSON)
		if req.URL.Path != "/apis/batch/v1beta1/namespaces/default/cronjobs" {
			t.Errorf("Unexpected URL %s", req.URL.Path)
		}
		return &http.Response{
			StatusCode: 200,
			Header:     header,
			Body:       objBody(nil),
		}, nil
	})
	err = EnsureCronJob(client, f1, c, or, "batch/v1beta1")
}

func doesNotContain(envs []v1.EnvVar, env v1.EnvVar) bool {
	for _, e := range envs {
		if e == env {
			return false
		}
	}
	return true
}

func TestCreateIngressResource(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	f1 := &kubelessApi.Function{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foo",
			Namespace: "myns",
			UID:       "1234",
		},
		Spec: kubelessApi.FunctionSpec{},
	}
	httpTrigger := &kubelessApi.HTTPTrigger{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foo",
			Namespace: "myns",
			UID:       "1234",
		},
		Spec: kubelessApi.HTTPTriggerSpec{
			ServiceSpec: v1.ServiceSpec{
				Ports: []v1.ServicePort{
					{
						TargetPort: intstr.FromInt(8080),
					},
				},
			},
			FunctionName: f1.Name,
		},
	}
	if err := CreateIngress(clientset, httpTrigger); err != nil {
		t.Fatalf("Creating ingress returned err: %v", err)
	}
	if err := CreateIngress(clientset, httpTrigger); err != nil {
		if !k8sErrors.IsAlreadyExists(err) {
			t.Fatalf("Expect object is already exists, got %v", err)
		}
	}
	httpTrigger.Spec.ServiceSpec.Ports = []v1.ServicePort{}
	if err := CreateIngress(clientset, httpTrigger); err == nil {
		t.Fatal("Expect create ingress fails, got success")
	}
}

func TestCreateIngressResourceWithTLSAcme(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	f1 := &kubelessApi.Function{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foo",
			Namespace: "myns",
			UID:       "1234",
		},
		Spec: kubelessApi.FunctionSpec{},
	}
	httpTrigger := &kubelessApi.HTTPTrigger{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foo",
			Namespace: "myns",
			UID:       "1234",
		},
		Spec: kubelessApi.HTTPTriggerSpec{
			ServiceSpec: v1.ServiceSpec{
				Ports: []v1.ServicePort{
					{
						TargetPort: intstr.FromInt(8080),
					},
				},
			},
			HostName:     "foo",
			RouteName:    "foo",
			TLSAcme:      true,
			FunctionName: f1.Name,
		},
	}
	if err := CreateIngress(clientset, httpTrigger); err != nil {
		t.Fatalf("Creating ingress returned err: %v", err)
	}

	ingress, err := clientset.ExtensionsV1beta1().Ingresses("myns").Get("foo", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Getting Ingress returned err: %v", err)
	}

	annotations := ingress.ObjectMeta.Annotations
	if annotations == nil || len(annotations) == 0 ||
		annotations["kubernetes.io/tls-acme"] != "true" ||
		annotations["ingress.kubernetes.io/ssl-redirect"] != "true" {
		t.Fatal("Missing or wrong annotations!")
	}

	tls := ingress.Spec.TLS
	if tls == nil || len(tls) != 1 ||
		tls[0].SecretName == "" ||
		tls[0].Hosts == nil || len(tls[0].Hosts) != 1 || tls[0].Hosts[0] == "" {
		t.Fatal("Missing or incomplete TLS spec!")
	}
}

func TestDeleteIngressResource(t *testing.T) {
	myNsFoo := metav1.ObjectMeta{
		Namespace: "myns",
		Name:      "foo",
	}

	ing := xv1beta1.Ingress{
		ObjectMeta: myNsFoo,
	}

	clientset := fake.NewSimpleClientset(&ing)
	if err := DeleteIngress(clientset, "foo", "myns"); err != nil {
		t.Fatalf("Deleting ingress returned err: %v", err)
	}
	a := clientset.Actions()
	if ns := a[0].GetNamespace(); ns != "myns" {
		t.Errorf("deleted ingress from wrong namespace (%s)", ns)
	}
	if name := a[0].(ktesting.DeleteAction).GetName(); name != "foo" {
		t.Errorf("deleted ingress with wrong name (%s)", name)
	}
}

func fakeConfig() *rest.Config {
	return &rest.Config{
		Host: "https://example.com:443",
		ContentConfig: rest.ContentConfig{
			GroupVersion: &schema.GroupVersion{
				Group:   "",
				Version: "v1",
			},
			NegotiatedSerializer: scheme.Codecs,
		},
	}
}

func TestGetLocalHostname(t *testing.T) {
	config := fakeConfig()
	expectedHostName := "foobar.example.com.nip.io"
	actualHostName, err := GetLocalHostname(config, "foobar")
	if err != nil {
		t.Error(err)
	}

	if expectedHostName != actualHostName {
		t.Errorf("Expected %s but got %s", expectedHostName, actualHostName)
	}
}

func TestCreateAutoscaleResource(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	name := "foo"
	ns := "myns"
	hpaDef := v2beta1.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
		},
	}
	if err := CreateAutoscale(clientset, hpaDef); err != nil {
		t.Fatalf("Creating autoscale returned err: %v", err)
	}

	hpa, err := clientset.AutoscalingV2beta1().HorizontalPodAutoscalers(ns).Get(name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Creating autoscale returned err: %v", err)
	}
	if hpa.ObjectMeta.Name != "foo" {
		t.Fatalf("Creating wrong scale target name")
	}
}

func TestDeleteAutoscaleResource(t *testing.T) {
	myNsFoo := metav1.ObjectMeta{
		Namespace: "myns",
		Name:      "foo",
	}

	as := v2beta1.HorizontalPodAutoscaler{
		ObjectMeta: myNsFoo,
	}

	clientset := fake.NewSimpleClientset(&as)
	if err := DeleteAutoscale(clientset, "foo", "myns"); err != nil {
		t.Fatalf("Deleting autoscale returned err: %v", err)
	}
	a := clientset.Actions()
	if ns := a[0].GetNamespace(); ns != "myns" {
		t.Errorf("deleted autoscale from wrong namespace (%s)", ns)
	}
	if name := a[0].(ktesting.DeleteAction).GetName(); name != "foo" {
		t.Errorf("deleted autoscale with wrong name (%s)", name)
	}
}

func TestGetProvisionContainer(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	langruntime.AddFakeConfig(clientset)
	lr := langruntime.SetupLangRuntime(clientset)
	lr.ReadConfigMap()

	rvol := v1.VolumeMount{Name: "runtime", MountPath: "/runtime"}
	dvol := v1.VolumeMount{Name: "deps", MountPath: "/deps"}
	c, err := getProvisionContainer("test", "sha256:abc1234", "test.func", "test.foo", "text", "python2.7", rvol, dvol, lr)
	if err != nil {
		t.Errorf("Unexpected error: %s", err)
	}
	expectedContainer := v1.Container{
		Name:            "prepare",
		Image:           "kubeless/unzip@sha256:f162c062973cca05459834de6ed14c039d45df8cdb76097f50b028a1621b3697",
		Command:         []string{"sh", "-c"},
		Args:            []string{"echo 'abc1234  /deps/test.func' > /deps/test.func.sha256 && sha256sum -c /deps/test.func.sha256 && cp /deps/test.func /runtime/test.py && cp /deps/requirements.txt /runtime"},
		VolumeMounts:    []v1.VolumeMount{rvol, dvol},
		ImagePullPolicy: v1.PullIfNotPresent,
	}
	if !reflect.DeepEqual(expectedContainer, c) {
		t.Errorf("Unexpected result:\n %+v", c)
	}

	// If the content type is encoded it should decode it
	c, err = getProvisionContainer("Zm9vYmFyCg==", "sha256:abc1234", "test.func", "test.foo", "base64", "python2.7", rvol, dvol, lr)
	if err != nil {
		t.Errorf("Unexpected error: %s", err)
	}
	if !strings.HasPrefix(c.Args[0], "base64 -d < /deps/test.func > /deps/test.func.decoded") {
		t.Errorf("Unexpected command: %s", c.Args[0])
	}

	secrets, err := lr.GetImageSecrets("python2.7")
	if err != nil {
		t.Errorf("Unable to fetch secrets: %v", err)
	}

	if secrets[0].Name != "p1" && secrets[1].Name != "p2" {
		t.Errorf("Expected first secret to be 'p1' but found %v and second secret to be 'p2' but found %v", secrets[0], secrets[1])
	}

	// It should skip the dependencies installation if the runtime is not supported
	c, err = getProvisionContainer("function", "sha256:abc1234", "test.func", "test.foo", "text", "cobol", rvol, dvol, lr)
	if err != nil {
		t.Errorf("Unexpected error: %s", err)
	}
	if strings.Contains(c.Args[0], "cp /deps ") {
		t.Errorf("Unexpected command: %s", c.Args[0])
	}

	// It should extract the file in case it is a Zip
	c, err = getProvisionContainer("Zm9vYmFyCg==", "sha256:abc1234", "test.zip", "test.foo", "base64+zip", "python2.7", rvol, dvol, lr)
	if !strings.Contains(c.Args[0], "unzip -o /deps/test.zip.decoded -d /runtime") {
		t.Errorf("Unexpected command: %s", c.Args[0])
	}

}

func TestInitializeEmptyMapsInDeployment(t *testing.T) {
	deployment := v1beta1.Deployment{}
	deployment.Spec.Selector = &metav1.LabelSelector{}
	initializeEmptyMapsInDeployment(&deployment)
	if deployment.ObjectMeta.Annotations == nil {
		t.Fatal("ObjectMeta.Annotations map is nil")
	}
	if deployment.ObjectMeta.Labels == nil {
		t.Fatal("ObjectMeta.Labels map is nil")
	}
	if deployment.Spec.Selector == nil && deployment.Spec.Selector.MatchLabels == nil {
		t.Fatal("deployment.Spec.Selector.MatchLabels is nil")
	}
	if deployment.Spec.Template.ObjectMeta.Labels == nil {
		t.Fatal("deployment.Spec.Template.ObjectMeta.Labels map is nil")
	}
	if deployment.Spec.Template.ObjectMeta.Annotations == nil {
		t.Fatal("deployment.Spec.Template.ObjectMeta.Annotations map is nil")
	}
	if deployment.Spec.Template.Spec.NodeSelector == nil {
		t.Fatal("deployment.Spec.Template.Spec.NodeSelector map is nil")
	}
}

func TestMergeDeployments(t *testing.T) {
	var replicas int32
	replicas = 10
	destinationDeployment := v1beta1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				"foo1-deploy": "bar",
			},
		},
	}

	sourceDeployment := v1beta1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				"foo2-deploy": "bar",
			},
		},
		Spec: v1beta1.DeploymentSpec{
			Replicas: &replicas,
		},
	}

	MergeDeployments(&destinationDeployment, &sourceDeployment)
	expectedAnnotations := map[string]string{
		"foo1-deploy": "bar",
		"foo2-deploy": "bar",
	}
	for i := range expectedAnnotations {
		if destinationDeployment.ObjectMeta.Annotations[i] != expectedAnnotations[i] {
			t.Fatalf("Expecting annotation %s but received %s", destinationDeployment.ObjectMeta.Annotations[i], expectedAnnotations[i])
		}
	}
	if *destinationDeployment.Spec.Replicas != replicas {
		t.Fatalf("Expecting replicas as 10 but received %v", *destinationDeployment.Spec.Replicas)
	}

}
