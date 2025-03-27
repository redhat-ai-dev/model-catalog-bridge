package rhoai_normalizer

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	serverapiv1beta1 "github.com/kserve/kserve/pkg/apis/serving/v1beta1"
	routev1 "github.com/openshift/api/route/v1"
	"github.com/redhat-ai-dev/model-catalog-bridge/pkg/cmd/cli/kubeflowmodelregistry"
	"github.com/redhat-ai-dev/model-catalog-bridge/pkg/config"
	types2 "github.com/redhat-ai-dev/model-catalog-bridge/pkg/types"
	"github.com/redhat-ai-dev/model-catalog-bridge/test/stub/common"
	"github.com/redhat-ai-dev/model-catalog-bridge/test/stub/kfmr"
	"github.com/redhat-ai-dev/model-catalog-bridge/test/stub/location"
	"github.com/redhat-ai-dev/model-catalog-bridge/test/stub/storage"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"net/http/httptest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sync"
	"testing"
)

func TestReconcile(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = serverapiv1beta1.AddToScheme(scheme)
	kts1 := kfmr.CreateGetServerWithInference(t)
	defer kts1.Close()
	kts2 := kfmr.CreateGetServer(t)
	defer kts2.Close()
	brts := location.CreateBridgeLocationServer(t)
	defer brts.Close()
	callback := sync.Map{}
	bsts := storage.CreateBridgeStorageREST(t, &callback)
	defer bsts.Close()

	r := &RHOAINormalizerReconcile{
		scheme:        scheme,
		eventRecorder: nil,
		k8sToken:      "",
		myNS:          "",
		routeClient:   nil,
		storage:       storage.SetupBridgeStorageRESTClient(bsts),
		format:        types2.JsonArrayForamt,
	}

	for _, tc := range []struct {
		name          string
		is            *serverapiv1beta1.InferenceService
		route         *routev1.Route
		kfmrSvr       *httptest.Server
		expectedFound bool
		expectedValue string
	}{
		{
			name: "kserve inference service without kubeflow route",
			is: &serverapiv1beta1.InferenceService{
				ObjectMeta: metav1.ObjectMeta{Namespace: "foo", Name: "bar"},
				Spec:       serverapiv1beta1.InferenceServiceSpec{},
				Status:     serverapiv1beta1.InferenceServiceStatus{},
			},
			//TODO set expectedFound to true and check for this expectedValue with kserve-only is re-added after summit
			//expectedValue: "KServe instance foo:bar",
		},
		{
			name: "kserve inference service with kubeflow route but not kubeflow inference service",
			route: &routev1.Route{
				Spec: routev1.RouteSpec{
					Host: "http://foo.com",
				},
				Status: routev1.RouteStatus{Ingress: []routev1.RouteIngress{{}}},
			},
			is: &serverapiv1beta1.InferenceService{
				ObjectMeta: metav1.ObjectMeta{Namespace: "faa", Name: "bor"},
				Spec:       serverapiv1beta1.InferenceServiceSpec{},
				Status:     serverapiv1beta1.InferenceServiceStatus{},
			},
			kfmrSvr: kts2,
			//TODO set expectedFound to true and check for this expectedValue with kserve-only is re-added after summit
			//expectedValue: "KServe instance faa:bor",
		},
		{
			name: "kserve inference service with kubeflow route and kubeflow inference service",
			route: &routev1.Route{
				Spec: routev1.RouteSpec{
					Host: "http://foo.com",
				},
				Status: routev1.RouteStatus{Ingress: []routev1.RouteIngress{{}}},
			},
			is: &serverapiv1beta1.InferenceService{
				ObjectMeta: metav1.ObjectMeta{Name: "mnist-v1", Namespace: "ggmtest"},
				Spec:       serverapiv1beta1.InferenceServiceSpec{},
				Status:     serverapiv1beta1.InferenceServiceStatus{},
			},
			kfmrSvr:       kts1,
			expectedFound: true,
			expectedValue: "https://huggingface.co/tarilabs/mnist/resolve/v20231206163028/mnist.onnx",
		},
	} {
		ctx := context.TODO()
		objs := []client.Object{tc.is}
		if tc.kfmrSvr != nil {
			cfg := &config.Config{}
			kfmr.SetupKubeflowTestRESTClient(tc.kfmrSvr, cfg)
			r.kfmr = kubeflowmodelregistry.SetupKubeflowRESTClient(cfg)
		}
		r.client = fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
		r.kfmrRoute = tc.route
		r.Reconcile(ctx, reconcile.Request{types.NamespacedName{Namespace: tc.is.Namespace, Name: tc.is.Name}})
		found := false
		callback.Range(func(key, value any) bool {
			found = true
			t.Logf(fmt.Sprintf("found key %s for test %s", key, tc.name))
			postStr, ok := value.(string)
			common.AssertEqual(t, ok, true)
			common.AssertContains(t, postStr, []string{tc.expectedValue})

			return true
		})
		common.AssertEqual(t, tc.expectedFound, found)
	}
}

func TestStart(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = serverapiv1beta1.AddToScheme(scheme)
	kts1 := kfmr.CreateGetServerWithInference(t)
	defer kts1.Close()
	kts2 := kfmr.CreateGetServer(t)
	defer kts2.Close()
	brts := location.CreateBridgeLocationServer(t)
	defer brts.Close()
	callback := sync.Map{}
	bsts := storage.CreateBridgeStorageREST(t, &callback)
	defer bsts.Close()

	r := &RHOAINormalizerReconcile{
		scheme:        scheme,
		eventRecorder: nil,
		k8sToken:      "",
		myNS:          "",
		routeClient:   nil,
		kfmrRoute: &routev1.Route{
			Spec: routev1.RouteSpec{
				Host: "http://foo.com",
			},
			Status: routev1.RouteStatus{Ingress: []routev1.RouteIngress{{}}},
		},
		storage: storage.SetupBridgeStorageRESTClient(bsts),
		//TODO for now letting TestReconcile handle Json Array and this handle catalog-info.yaml, but eventually may just do json array everywhere
		format: types2.CatalogInfoYamlFormat,
	}

	for _, tc := range []struct {
		name          string
		is            *serverapiv1beta1.InferenceService
		kfmrSvr       *httptest.Server
		expectedKey   string
		expectedValue string
	}{
		{
			name: "not deployed, only registered model, model version, model artifact",
			is: &serverapiv1beta1.InferenceService{
				ObjectMeta: metav1.ObjectMeta{Namespace: "faa", Name: "bor"},
				Spec:       serverapiv1beta1.InferenceServiceSpec{},
				Status:     serverapiv1beta1.InferenceServiceStatus{},
			},
			kfmrSvr:       kts2,
			expectedKey:   "/model-1/v1/catalog-info.yaml",
			expectedValue: "description: dummy model 1",
		},
		{
			name: "deployed, with inference_service and serving_environments added",
			is: &serverapiv1beta1.InferenceService{
				ObjectMeta: metav1.ObjectMeta{Name: "mnist-v1", Namespace: "ggmtest"},
				Spec:       serverapiv1beta1.InferenceServiceSpec{},
				Status:     serverapiv1beta1.InferenceServiceStatus{},
			},
			kfmrSvr:       kts1,
			expectedKey:   "/mnist/v1/catalog-info.yaml",
			expectedValue: "url: https://huggingface.co/tarilabs/mnist/resolve/v20231206163028/mnist.onnx",
		},
	} {
		ctx := context.TODO()
		objs := []client.Object{tc.is}
		if tc.kfmrSvr != nil {
			cfg := &config.Config{}
			kfmr.SetupKubeflowTestRESTClient(tc.kfmrSvr, cfg)
			r.kfmr = kubeflowmodelregistry.SetupKubeflowRESTClient(cfg)
		}
		r.client = fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()

		b := []byte{}
		buf := bytes.NewBuffer(b)
		bwriter := bufio.NewWriter(buf)
		r.innerStart(ctx, buf, bwriter)

		found := false
		callback.Range(func(key, value any) bool {
			found = true
			t.Logf(fmt.Sprintf("found key %s for test %s", key, tc.name))
			postStr, ok := value.(string)
			common.AssertEqual(t, ok, true)
			common.AssertContains(t, postStr, []string{tc.expectedValue})

			return true
		})
		common.AssertEqual(t, found, true)
		common.AssertEqual(t, true, len(buf.Bytes()) > 0)
	}

}

func TestStartArchived(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = serverapiv1beta1.AddToScheme(scheme)
	kts1 := kfmr.CreateGetServerArchived(t)
	defer kts1.Close()
	brts := location.CreateBridgeLocationServer(t)
	defer brts.Close()
	callback := sync.Map{}
	bsts := storage.CreateBridgeStorageREST(t, &callback)
	defer bsts.Close()

	r := &RHOAINormalizerReconcile{
		scheme:        scheme,
		eventRecorder: nil,
		k8sToken:      "",
		myNS:          "",
		routeClient:   nil,
		kfmrRoute: &routev1.Route{
			Spec: routev1.RouteSpec{
				Host: "http://foo.com",
			},
			Status: routev1.RouteStatus{Ingress: []routev1.RouteIngress{{}}},
		},
		storage: storage.SetupBridgeStorageRESTClient(bsts),
		//TODO eventually switch the defaulting to json array
		format: types2.CatalogInfoYamlFormat,
	}

	for _, tc := range []struct {
		name string
	}{
		{
			name: "not deployed, only registered model, model version, model artifact",
		},
	} {
		ctx := context.TODO()
		cfg := &config.Config{}
		kfmr.SetupKubeflowTestRESTClient(kts1, cfg)
		r.kfmr = kubeflowmodelregistry.SetupKubeflowRESTClient(cfg)
		r.client = fake.NewClientBuilder().WithScheme(scheme).Build()

		b := []byte{}
		buf := bytes.NewBuffer(b)
		bwriter := bufio.NewWriter(buf)
		r.innerStart(ctx, buf, bwriter)

		found := false
		callback.Range(func(key, value any) bool {
			found = true
			t.Logf(fmt.Sprintf("found key %s value %s for test %s", key, value, tc.name))

			return true
		})
		// callback should not have any entries since we should not have called the storage tier
		common.AssertEqual(t, found, false)
		common.AssertEqual(t, true, len(buf.Bytes()) == 0)
	}

}
