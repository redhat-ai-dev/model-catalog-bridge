package kubeflowmodelregistry

import (
	"bufio"
	"bytes"
	serverapiv1beta1 "github.com/kserve/kserve/pkg/apis/serving/v1beta1"
	"github.com/redhat-ai-dev/model-catalog-bridge/pkg/config"
	"github.com/redhat-ai-dev/model-catalog-bridge/pkg/types"
	"github.com/redhat-ai-dev/model-catalog-bridge/test/stub/common"
	"github.com/redhat-ai-dev/model-catalog-bridge/test/stub/kfmr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"knative.dev/pkg/apis"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"testing"
)

func TestLoopOverKRMR_JsonArray(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = serverapiv1beta1.AddToScheme(scheme)
	ts := kfmr.CreateGetServerWithInference(t)
	defer ts.Close()
	for _, tc := range []struct {
		args []string
		// we do output compare in chunks as ranges over the components status map are non-deterministic wrt order
		outStr []string
		is     *serverapiv1beta1.InferenceService
	}{
		{
			args:   []string{"Owner", "Lifecycle"},
			outStr: []string{jsonListOutputJSON},
		},
		{
			args:   []string{"Owner", "Lifecycle", "1"},
			outStr: []string{jsonListOutputJSON},
		},
		{
			args:   []string{"Owner", "Lifecycle"},
			outStr: []string{jsonListWithInferenceOutputJSON},
			is: &serverapiv1beta1.InferenceService{
				ObjectMeta: metav1.ObjectMeta{
					// see test/stub/common/MnistInferenceServices and test/stub/common/MinstServingEnvironment
					Namespace: "ggmtest",
					Name:      "mnist-v1",
				},
				Spec: serverapiv1beta1.InferenceServiceSpec{},
				Status: serverapiv1beta1.InferenceServiceStatus{URL: &apis.URL{
					Scheme: "https",
					Host:   "kserve.com",
				}},
			},
		},
	} {
		cfg := &config.Config{}
		kfmr.SetupKubeflowTestRESTClient(ts, cfg)
		k := SetupKubeflowRESTClient(cfg)
		owner := tc.args[0]
		lifecycle := tc.args[1]
		ids := []string{}
		if len(tc.args) > 2 {
			ids = tc.args[2:]
		}
		b := []byte{}
		buf := bytes.NewBuffer(b)
		bwriter := bufio.NewWriter(buf)
		var cl client.Client
		if tc.is != nil {
			objs := []client.Object{tc.is}
			cl = fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
		}
		_, _, err := LoopOverKFMR(owner, lifecycle, ids, bwriter, types.JsonArrayForamt, k, cl)
		common.AssertError(t, err)
		bwriter.Flush()
		outstr := buf.String()
		for _, str := range tc.outStr {
			common.AssertLineCompare(t, outstr, str, 0)
		}

	}
}

func TestLoopOverKFMR_CatalogInfoYaml(t *testing.T) {
	ts := kfmr.CreateGetServer(t)
	defer ts.Close()
	for _, tc := range []struct {
		args []string
		// we do output compare in chunks as ranges over the components status map are non-deterministic wrt order
		outStr []string
	}{
		{
			args:   []string{"Owner", "Lifecycle"},
			outStr: []string{listOutput},
		},
		{
			args:   []string{"Owner", "Lifecycle", "1"},
			outStr: []string{listOutput},
		},
	} {
		cfg := &config.Config{}
		kfmr.SetupKubeflowTestRESTClient(ts, cfg)
		k := SetupKubeflowRESTClient(cfg)
		owner := tc.args[0]
		lifecycle := tc.args[1]
		ids := []string{}
		if len(tc.args) > 2 {
			ids = tc.args[2:]
		}
		b := []byte{}
		buf := bytes.NewBuffer(b)
		bwriter := bufio.NewWriter(buf)
		_, _, err := LoopOverKFMR(owner, lifecycle, ids, bwriter, types.CatalogInfoYamlFormat, k, nil)
		common.AssertError(t, err)
		bwriter.Flush()
		outstr := buf.String()
		for _, str := range tc.outStr {
			common.AssertLineCompare(t, outstr, str, 0)
		}

	}

}

const (
	jsonListWithInferenceOutputJSON = `{"models":[{"artifactLocationURL":"https://huggingface.co/tarilabs/mnist/resolve/v20231206163028/mnist.onnx","description":"","lifecycle":"Lifecycle","name":"v1","owner":"rhdh-rhoai-bridge","tags":["_lastModified"]}],"modelServer":{"API":{"spec":"","type":"openapi","url":"https://kserve.com"},"authentication":false,"description":"","lifecycle":"development","name":"mnist-v1/8c2c357f-bf82-4d2d-a254-43eca96fd31d","owner":"rhdh-rhoai-bridge","tags":["_lastModified"]}}`
	jsonListWithInferenceOutputYAML = `modelServer:
  API:
    spec: ""
    type: openapi
    url: https://kserve.com
  authentication: false
  description: ""
  lifecycle: development
  name: mnist-v1/8c2c357f-bf82-4d2d-a254-43eca96fd31d
  owner: rhdh-rhoai-bridge
  tags:
  - _lastModified
models:
- artifactLocationURL: https://huggingface.co/tarilabs/mnist/resolve/v20231206163028/mnist.onnx
  description: ""
  lifecycle: Lifecycle
  name: v1
  owner: rhdh-rhoai-bridge
  tags:
  - _lastModified`
	jsonListOutputJSON = `{"models":[{"artifactLocationURL":"https://huggingface.co/tarilabs/mnist/resolve/v20231206163028/mnist.onnx","description":"","lifecycle":"Lifecycle","name":"v1","owner":"rhdh-rhoai-bridge","tags":["_lastModified"]}]}`
	jsonListOutputYAML = `models:
- artifactLocationURL: https://huggingface.co/tarilabs/mnist/resolve/v20231206163028/mnist.onnx
  description: ""
  lifecycle: Lifecycle
  name: v1
  owner: rhdh-rhoai-bridge
  tags:
  - _lastModified
`
	listOutput = `apiVersion: backstage.io/v1alpha1
kind: Component
metadata:
  annotations:
    backstage.io/techdocs-ref: ./
  description: dummy model 1
  links:
  - icon: WebAsset
    title: version 1
    type: website
    url: https://foo.com
  name: model-1
  tags:
  - foo-bar
spec:
  dependsOn:
  - resource:v1
  - api:model-1-v1-artifact
  lifecycle: Lifecycle
  owner: user:kube:admin
  profile:
    displayName: model-1
  type: model-server
---
apiVersion: backstage.io/v1alpha1
kind: Resource
metadata:
  annotations:
    backstage.io/techdocs-ref: resource/
  description: dummy model 1
  links:
  - icon: WebAsset
    title: version 1
    type: website
    url: https://foo.com
  name: v1
spec:
  dependencyOf:
  - component:model-1
  lifecycle: Lifecycle
  owner: user:kube:admin
  profile:
    displayName: v1
  type: ai-model
---
apiVersion: backstage.io/v1alpha1
kind: API
metadata:
  annotations:
    backstage.io/techdocs-ref: api/
  description: dummy model 1
  name: model-1
spec:
  definition: no-definition-yet
  dependencyOf:
  - component:model-1
  lifecycle: Lifecycle
  owner: user:kube:admin
  profile:
    displayName: model-1
  type: unknown
`
)
