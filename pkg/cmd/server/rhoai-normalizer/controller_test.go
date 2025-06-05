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
	bridgerest "github.com/redhat-ai-dev/model-catalog-bridge/pkg/rest"
	types2 "github.com/redhat-ai-dev/model-catalog-bridge/pkg/types"
	"github.com/redhat-ai-dev/model-catalog-bridge/test/stub/common"
	"github.com/redhat-ai-dev/model-catalog-bridge/test/stub/kfmr"
	"github.com/redhat-ai-dev/model-catalog-bridge/test/stub/location"
	"github.com/redhat-ai-dev/model-catalog-bridge/test/stub/storage"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"knative.dev/pkg/apis"
	duckv1 "knative.dev/pkg/apis/duck/v1"
	"net/http/httptest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"strings"
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
			name: "kserve inference service that is not yet ready",
			is: &serverapiv1beta1.InferenceService{
				ObjectMeta: metav1.ObjectMeta{Namespace: "foo", Name: "bar"},
				Spec:       serverapiv1beta1.InferenceServiceSpec{},
				Status:     serverapiv1beta1.InferenceServiceStatus{},
			},
		},
		{
			name: "kserve inference service without kubeflow route",
			is: &serverapiv1beta1.InferenceService{
				ObjectMeta: metav1.ObjectMeta{Namespace: "foo", Name: "bar"},
				Spec:       serverapiv1beta1.InferenceServiceSpec{},
				Status: serverapiv1beta1.InferenceServiceStatus{
					ModelStatus: serverapiv1beta1.ModelStatus{
						TransitionStatus: serverapiv1beta1.UpToDate,
					},
					Status: duckv1.Status{
						Conditions: duckv1.Conditions{
							{
								Type:   bridgerest.INF_SVC_IngressReady_CONDITION,
								Status: corev1.ConditionTrue,
							},
							{
								Type:   bridgerest.INF_SVC_PredictorReady_CONDITION,
								Status: corev1.ConditionTrue,
							},
							{
								Type:   bridgerest.INF_SVC_Ready_CONDITION,
								Status: corev1.ConditionTrue,
							},
						},
					},
					URL: &apis.URL{
						Scheme: "https",
						Host:   "kserve.com",
					},
				},
			},
			expectedFound: true,
			expectedValue: `"owner":"foo"`,
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
				Status: serverapiv1beta1.InferenceServiceStatus{
					ModelStatus: serverapiv1beta1.ModelStatus{
						TransitionStatus: serverapiv1beta1.UpToDate,
					},
					Status: duckv1.Status{
						Conditions: duckv1.Conditions{
							{
								Type:   bridgerest.INF_SVC_IngressReady_CONDITION,
								Status: corev1.ConditionTrue,
							},
							{
								Type:   bridgerest.INF_SVC_PredictorReady_CONDITION,
								Status: corev1.ConditionTrue,
							},
							{
								Type:   bridgerest.INF_SVC_Ready_CONDITION,
								Status: corev1.ConditionTrue,
							},
						},
					},
					URL: &apis.URL{
						Scheme: "https",
						Host:   "kserve.com",
					},
				},
			},
			kfmrSvr:       kts2,
			expectedFound: true,
			expectedValue: `"owner":"faa"`,
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
		r.kfmrRoute = map[string]*routev1.Route{}
		r.kfmr = map[string]*kubeflowmodelregistry.KubeFlowRESTClientWrapper{}
		if tc.kfmrSvr != nil {
			cfg := &config.Config{}
			kfmr.SetupKubeflowTestRESTClient(tc.kfmrSvr, cfg)
			r.kfmr[tc.name] = kubeflowmodelregistry.SetupKubeflowRESTClient(cfg)
		}
		r.client = fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
		if tc.route != nil {
			r.kfmrRoute[tc.name] = tc.route
		}
		result, err := r.Reconcile(ctx, reconcile.Request{types.NamespacedName{Namespace: tc.is.Namespace, Name: tc.is.Name}})
		common.AssertError(t, err)
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
		if !tc.expectedFound {
			common.AssertEqual(t, result.Requeue, true)
		}
	}
}

func TestStart(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = serverapiv1beta1.AddToScheme(scheme)
	kts1 := kfmr.CreateGetServerWithInference(t)
	defer kts1.Close()
	kts2 := kfmr.CreateGetServer(t)
	defer kts2.Close()
	kts3 := kfmr.CreateEmptyGetServer(t)
	defer kts3.Close()
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
		kfmrRoute: map[string]*routev1.Route{
			"foo": &routev1.Route{
				Spec: routev1.RouteSpec{
					Host: "http://foo.com",
				},
				Status: routev1.RouteStatus{Ingress: []routev1.RouteIngress{{}}},
			},
		},
		storage: storage.SetupBridgeStorageRESTClient(bsts),
		// letting TestReconcile handle Json Array and this handle catalog-info.yaml, as it is better suited for testing our output buffer with multiple registries;
		// remember, Reconcile will only produced one ModelCatalog, while the background poll can produce multiple, we pass a writer/buffer to collect all the entries
		format: types2.CatalogInfoYamlFormat,
	}

	for _, tc := range []struct {
		name          string
		is            *serverapiv1beta1.InferenceService
		kfmrSvr       []*httptest.Server
		expectedKey   string
		expectedValue []string
	}{
		{
			name: "not deployed, only registered model, model version, model artifact",
			is: &serverapiv1beta1.InferenceService{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "faa",
					Name:      "bor",
					Labels:    map[string]string{bridgerest.INF_SVC_RM_ID_LABEL: "1"},
				},
				Spec:   serverapiv1beta1.InferenceServiceSpec{},
				Status: serverapiv1beta1.InferenceServiceStatus{},
			},
			kfmrSvr:       []*httptest.Server{kts2},
			expectedKey:   "model-1_v1",
			expectedValue: []string{"description: dummy model 1"},
		},
		{
			name: "deployed, with inference_service and serving_environments added",
			is: &serverapiv1beta1.InferenceService{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "mnist-v1",
					Namespace: "ggmtest",
					Labels:    map[string]string{bridgerest.INF_SVC_RM_ID_LABEL: "1"},
				},
				Spec:   serverapiv1beta1.InferenceServiceSpec{},
				Status: serverapiv1beta1.InferenceServiceStatus{},
			},
			kfmrSvr:       []*httptest.Server{kts1},
			expectedKey:   "mnist_v1,mnist_v3",
			expectedValue: []string{"url: https://huggingface.co/tarilabs/mnist/resolve/v20231206163028/mnist.onnx"},
		},
		{
			name: "deployed with multiple registries, with inference_service and serving_environments added, but also not deployed, only registered model, model version, model artifact",
			is: &serverapiv1beta1.InferenceService{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "mnist-v1",
					Namespace: "ggmtest",
					Labels:    map[string]string{bridgerest.INF_SVC_RM_ID_LABEL: "1"},
				},
				Spec:   serverapiv1beta1.InferenceServiceSpec{},
				Status: serverapiv1beta1.InferenceServiceStatus{},
			},
			kfmrSvr:       []*httptest.Server{kts1, kts2},
			expectedKey:   "mnist_v1,mnist_v3,model-1_v1",
			expectedValue: []string{"url: https://huggingface.co/tarilabs/mnist/resolve/v20231206163028/mnist.onnx", "description: dummy model 1"},
		},
		{
			name: "deployed, kserve only, no labels",
			is: &serverapiv1beta1.InferenceService{
				ObjectMeta: metav1.ObjectMeta{Name: "mnist-v1", Namespace: "ggmtest"},
				Spec:       serverapiv1beta1.InferenceServiceSpec{},
				Status:     serverapiv1beta1.InferenceServiceStatus{},
			},
			kfmrSvr:     []*httptest.Server{kts3},
			expectedKey: "ggmtest_mnist-v1",
		},
		{
			name: "deployed, kserve only, non kubeflow labels",
			is: &serverapiv1beta1.InferenceService{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "mnist-v1",
					Namespace: "ggmtest",
					Labels:    map[string]string{"foo": "bar"},
				},
				Spec:   serverapiv1beta1.InferenceServiceSpec{},
				Status: serverapiv1beta1.InferenceServiceStatus{},
			},
			kfmrSvr:     []*httptest.Server{kts3},
			expectedKey: "ggmtest_mnist-v1",
		},
	} {
		ctx := context.TODO()
		objs := []client.Object{tc.is}
		r.kfmrRoute = map[string]*routev1.Route{}
		r.kfmr = map[string]*kubeflowmodelregistry.KubeFlowRESTClientWrapper{}
		for i, kfmrSvr := range tc.kfmrSvr {
			cfg := &config.Config{}
			kfmr.SetupKubeflowTestRESTClient(kfmrSvr, cfg)
			r.kfmr[fmt.Sprintf("%s-%d", tc.name, i)] = kubeflowmodelregistry.SetupKubeflowRESTClient(cfg)
		}
		r.client = fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()

		b := []byte{}
		buf := bytes.NewBuffer(b)
		bwriter := bufio.NewWriter(buf)
		r.innerStart(ctx, buf, bwriter)

		for _, expectedValue := range tc.expectedValue {
			found := false
			callback.Range(func(key, value any) bool {
				t.Logf(fmt.Sprintf("found key %s for test %s", key, tc.name))
				if !found {
					postStr, ok := value.(string)
					common.AssertEqual(t, ok, true)
					if strings.Contains(postStr, expectedValue) {
						found = true
					}
				}

				return true
			})
			common.AssertEqual(t, true, found)
			common.AssertEqual(t, true, len(buf.Bytes()) > 0)
		}
		if len(tc.expectedKey) > 0 {
			found := false
			callback.Range(func(key, value any) bool {
				t.Logf(fmt.Sprintf("found key %s for test %s", key, tc.name))
				if !found {
					postStr, ok := value.(string)
					common.AssertEqual(t, ok, true)
					if strings.Contains(postStr, tc.expectedKey) {
						found = true
					}
				}
				return true
			})
			common.AssertEqual(t, true, found)
		}
		// clear out callback for next test
		callback.Range(func(key, value any) bool {
			callback.Delete(key)
			return true
		})
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
		kfmrRoute: map[string]*routev1.Route{
			"foo": &routev1.Route{
				Spec: routev1.RouteSpec{
					Host: "http://foo.com",
				},
				Status: routev1.RouteStatus{Ingress: []routev1.RouteIngress{{}}},
			},
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
		r.kfmrRoute = map[string]*routev1.Route{}
		r.kfmr = map[string]*kubeflowmodelregistry.KubeFlowRESTClientWrapper{}
		kfmr.SetupKubeflowTestRESTClient(kts1, cfg)
		r.kfmr[tc.name] = kubeflowmodelregistry.SetupKubeflowRESTClient(cfg)
		r.client = fake.NewClientBuilder().WithScheme(scheme).Build()

		b := []byte{}
		buf := bytes.NewBuffer(b)
		bwriter := bufio.NewWriter(buf)
		r.innerStart(ctx, buf, bwriter)

		ok := false
		callback.Range(func(key, value any) bool {
			postStr, isStr := value.(string)
			common.AssertEqual(t, isStr, true)
			if len(postStr) == 0 {
				ok = true
			}
			t.Logf(fmt.Sprintf("found key %s value %s for test %s", key, value, tc.name))

			return true
		})
		// callback should not have any entries since we should not have called the storage tier
		common.AssertEqual(t, ok, true)
		common.AssertEqual(t, true, len(buf.Bytes()) == 0)
	}

}
