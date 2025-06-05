package rhoai_normalizer

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"github.com/go-logr/logr"
	"github.com/go-resty/resty/v2"
	serverapiv1beta1 "github.com/kserve/kserve/pkg/apis/serving/v1beta1"
	"github.com/kserve/kserve/pkg/constants"
	"github.com/kubeflow/model-registry/pkg/openapi"
	routev1 "github.com/openshift/api/route/v1"
	routeclient "github.com/openshift/client-go/route/clientset/versioned/typed/route/v1"
	"github.com/redhat-ai-dev/model-catalog-bridge/pkg/cmd/cli/kserve"
	"github.com/redhat-ai-dev/model-catalog-bridge/pkg/cmd/cli/kubeflowmodelregistry"
	"github.com/redhat-ai-dev/model-catalog-bridge/pkg/cmd/server/storage"
	bridgerest "github.com/redhat-ai-dev/model-catalog-bridge/pkg/rest"
	types2 "github.com/redhat-ai-dev/model-catalog-bridge/pkg/types"
	"github.com/redhat-ai-dev/model-catalog-bridge/pkg/util"
	"io"
	corev1 "k8s.io/api/core/v1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	k8sscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	"net/http"
	"os"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"strings"
	"time"
)

var (
	controllerLog = ctrl.Log.WithName("controller")
)

func NewControllerManager(ctx context.Context, cfg *rest.Config, options ctrl.Options, pprofAddr string) (ctrl.Manager, error) {
	apiextensionsClient := apiextensionsclient.NewForConfigOrDie(cfg)
	kserveClient := util.GetKServeClient(cfg)

	if err := wait.PollImmediate(time.Second*5, time.Minute*5, func() (done bool, err error) {
		crdName := fmt.Sprintf("%s.%s", constants.InferenceServiceAPIName, constants.KServeAPIGroupName)
		_, err = apiextensionsClient.ApiextensionsV1().CustomResourceDefinitions().Get(context.TODO(), crdName, metav1.GetOptions{})
		if err != nil {
			controllerLog.Error(err, "get of inferenceservices crd failed")
			return false, nil
		}

		_, err = kserveClient.InferenceServices("").List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			controllerLog.Error(err, "list of inferenceservers failed")
			return false, nil
		}

		controllerLog.Info("list of inferenceservices successful")
		return true, nil
	}); err != nil {
		controllerLog.Error(err, "waiting of inferenceservice CRD to be created")
		return nil, err
	}

	options.Scheme = runtime.NewScheme()
	if err := k8sscheme.AddToScheme(options.Scheme); err != nil {
		return nil, err
	}
	if err := serverapiv1beta1.AddToScheme(options.Scheme); err != nil {
		return nil, err
	}

	mgr, err := ctrl.NewManager(cfg, options)

	err = SetupController(ctx, mgr, cfg, pprofAddr)
	return mgr, err
}

// pprof enablement is OK running in production by default (i.e. you don't do CPU profiling and it allows us
// to get goroutine dumps if we have to diagnose deadlocks and the like

type pprof struct {
	port string
}

func (p *pprof) Start(ctx context.Context) error {
	srv := &http.Server{Addr: ":" + p.port}
	controllerLog.Info(fmt.Sprintf("starting ppprof on %s", p.port))
	go func() {
		err := srv.ListenAndServe()
		if err != nil {
			controllerLog.Info(fmt.Sprintf("pprof server err: %s", err.Error()))
		}
	}()
	<-ctx.Done()
	controllerLog.Info("Shutting down pprof")
	srv.Shutdown(ctx)
	return nil
}

func (r *RHOAINormalizerReconcile) setupKFMR(ctx context.Context) bool {
	var err error
	rr := strings.NewReplacer("\r", "", "\n", "")
	mrRoute := os.Getenv(types2.ModelRegistryRouteEnvVar)
	rr.Replace(mrRoute)
	routeTuples := strings.Split(mrRoute, ",")
	for _, routeTuple := range routeTuples {
		if len(routeTuple) == 0 {
			continue
		}
		parts := strings.Split(routeTuple, ":")
		kfmrRoute := &routev1.Route{}
		ns := "istio-system"
		name := parts[0]
		if len(parts) > 1 {
			ns = parts[0]
			name = parts[1]
		}
		kfmrRoute, err = r.routeClient.Routes(ns).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			controllerLog.Error(err, "error fetching model registry route")
			continue
		}
		if len(kfmrRoute.Status.Ingress) > 0 {
			r.kfmrRoute[routeTuple] = kfmrRoute
		}
	}

	if len(r.kfmrRoute) == 0 {
		return false
	}

	kfmrToken := os.Getenv(types2.ModelRegistryTokenEnvVar)
	kfmrToken = rr.Replace(kfmrToken)
	if len(kfmrToken) == 0 {
		kfmrToken = r.k8sToken
	}
	for key, kfmrRoute := range r.kfmrRoute {
		_, ok := r.kfmr[key]
		if ok {
			continue
		}
		kfmr := &kubeflowmodelregistry.KubeFlowRESTClientWrapper{
			Token:      kfmrToken,
			RootURL:    "https://" + kfmrRoute.Status.Ingress[0].Host + bridgerest.KFMR_BASE_URI,
			RESTClient: resty.New(),
		}
		kfmr.RESTClient.SetTLSClientConfig(&tls.Config{InsecureSkipVerify: true})
		r.kfmr[key] = kfmr
	}
	return true
}

func SetupController(ctx context.Context, mgr ctrl.Manager, cfg *rest.Config, pprofPort string) error {
	filter := &RHOAINormalizerFilter{}
	formatEnv := os.Getenv(types2.FormatEnvVar)
	r := strings.NewReplacer("\r", "", "\n", "")
	formatEnv = r.Replace(formatEnv)
	storageURL := os.Getenv(types2.StorageUrlEnvVar)
	storageURL = r.Replace(storageURL)
	if len(storageURL) == 0 {
		podIP := os.Getenv(util.PodIPEnvVar)
		podIP = r.Replace(podIP)
		storageURL = fmt.Sprintf("http://%s:7070", podIP)
		klog.Infof("using %s for the storage URL per our sidecar hack", storageURL)
	}
	reconciler := &RHOAINormalizerReconcile{
		client:        mgr.GetClient(),
		scheme:        mgr.GetScheme(),
		eventRecorder: mgr.GetEventRecorderFor("RHOAINormalizer"),
		k8sToken:      util.GetCurrentToken(cfg),
		routeClient:   routeclient.NewForConfigOrDie(cfg),
		storage:       storage.SetupBridgeStorageRESTClient(storageURL, util.GetCurrentToken(cfg)),
		format:        types2.NormalizerFormat(formatEnv),
		pollingInt:    2 * time.Minute,
	}

	polling := os.Getenv(types2.PollingIntEnvVar)
	pollingDuration, err := time.ParseDuration(polling)
	if err == nil && len(polling) > 0 {
		reconciler.pollingInt = pollingDuration
	}

	defaultOwner := os.Getenv(types2.OwnerEnvVar)
	defaultOwner = util.SanitizeName(defaultOwner)
	if len(defaultOwner) == 0 {
		defaultOwner = util.DefaultOwner
	}
	reconciler.defaultOwner = defaultOwner
	defaultLifecycle := os.Getenv(types2.LifecycleEnvVar)
	defaultLifecycle = util.SanitizeName(defaultLifecycle)
	if len(defaultLifecycle) == 0 {
		defaultLifecycle = util.DefaultLifecycle
	}
	reconciler.defaultLifecycle = defaultLifecycle

	switch reconciler.format {
	case types2.JsonArrayForamt:
	case types2.CatalogInfoYamlFormat:
	default:
		//TODO eventually switch the defaulting to json array
		reconciler.format = types2.CatalogInfoYamlFormat
	}

	reconciler.myNS = util.GetCurrentProject()

	reconciler.kfmrRoute = map[string]*routev1.Route{}
	reconciler.kfmr = map[string]*kubeflowmodelregistry.KubeFlowRESTClientWrapper{}
	reconciler.setupKFMR(ctx)

	err = ctrl.NewControllerManagedBy(mgr).For(&serverapiv1beta1.InferenceService{}).
		WithOptions(controller.Options{MaxConcurrentReconciles: 32}).
		WithEventFilter(filter).
		Complete(reconciler)
	if err != nil {
		return err
	}

	err = mgr.Add(reconciler)
	if err != nil {
		return err
	}

	if len(pprofPort) > 0 {
		pp := &pprof{port: pprofPort}
		err = mgr.Add(pp)
		if err != nil {
			return err
		}
	}

	return nil
}

type RHOAINormalizerFilter struct {
}

func (f *RHOAINormalizerFilter) Generic(event.GenericEvent) bool {
	return false
}

func (f *RHOAINormalizerFilter) Create(event.CreateEvent) bool {
	return true
}

func (f *RHOAINormalizerFilter) Delete(event.DeleteEvent) bool {
	return true
}

func (f RHOAINormalizerFilter) Update(e event.UpdateEvent) bool {
	return true
}

type RHOAINormalizerReconcile struct {
	client           client.Client
	scheme           *runtime.Scheme
	eventRecorder    record.EventRecorder
	k8sToken         string
	kfmrRoute        map[string]*routev1.Route
	myNS             string
	routeClient      *routeclient.RouteV1Client
	kfmr             map[string]*kubeflowmodelregistry.KubeFlowRESTClientWrapper
	storage          *storage.BridgeStorageRESTClient
	format           types2.NormalizerFormat
	defaultOwner     string
	defaultLifecycle string
	pollingInt       time.Duration
}

func (r *RHOAINormalizerReconcile) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	var cancel context.CancelFunc
	ctx, cancel = context.WithCancel(ctx)
	defer cancel()
	log := log.FromContext(ctx)

	is := &serverapiv1beta1.InferenceService{}
	name := types.NamespacedName{Namespace: request.Namespace, Name: request.Name}
	err := r.client.Get(ctx, name, is)
	if err != nil && !errors.IsNotFound(err) {
		return reconcile.Result{}, err
	}

	if err != nil {
		log.V(4).Info(fmt.Sprintf("initiating delete processing for %s", name.String()))
		// now, delete of the inference service does not mean the model has been deleted from model registry,
		// so we don't remove the model from our catalog, but may simply remove the URL associated with the inference
		// service; initiate a KFMR poll to remove any URLs/route from the model entries which depended on the inference service here; also,
		// if the delete of the kserve inference service happened to result from an archiving of the model, the
		// innerStart call will detect that and then initiate removal of the model from the storage and location services
		b := []byte{}
		buf := bytes.NewBuffer(b)
		bwriter := bufio.NewWriter(buf)
		r.innerStart(ctx, buf, bwriter)

		return reconcile.Result{}, nil
	}

	b := []byte{}
	buf := bytes.NewBuffer(b)
	bwriter := bufio.NewWriter(buf)
	importKey := ""

	//TODO fill in lifecycle from kfmr k/v pairs perhaps
	if len(r.kfmrRoute) > 0 {
		importKey, err = r.processKFMR(ctx, name, is, bwriter, log)
		if err != nil {
			return reconcile.Result{}, err
		}
	}
	if len(importKey) == 0 {
		// KServe only

		// let's wait for the status to reach a functional, ready state; aside from not exposing unusable models,
		// this will avoid any initial timing issues with model registry wiring (DB storage or
		// label setting), to be sure this is not a model registry created inference service
		if len(is.Status.Conditions) == 0 {
			return reconcile.Result{Requeue: true}, nil
		}
		if is.Status.ModelStatus.TransitionStatus != serverapiv1beta1.UpToDate {
			return reconcile.Result{Requeue: true}, nil
		}
		for _, condition := range is.Status.Conditions {
			switch {
			case condition.Type == bridgerest.INF_SVC_IngressReady_CONDITION:
				fallthrough
			case condition.Type == bridgerest.INF_SVC_PredictorReady_CONDITION:
				fallthrough
			case condition.Type == bridgerest.INF_SVC_Ready_CONDITION:
				if condition.Status != corev1.ConditionTrue {
					return reconcile.Result{Requeue: true}, nil
				}
			}
		}
		if is.Status.URL == nil {
			return reconcile.Result{Requeue: true}, nil
		}

		err = kserve.CallBackstagePrinters(ctx, is.Namespace, "developement", is, r.client, bwriter, r.format)

		if err != nil {
			return reconcile.Result{}, nil
		}

		importKey, _ = util.BuildImportKeyAndURI(util.SanitizeName(is.Namespace), util.SanitizeName(is.Name), r.format)
	}

	err = r.processBWriter(bwriter, buf, importKey)
	if err != nil {
		return reconcile.Result{}, err
	}

	return reconcile.Result{}, nil
}

func (r *RHOAINormalizerReconcile) processBWriter(bwriter *bufio.Writer, buf *bytes.Buffer, importKey string) error {
	err := bwriter.Flush()
	if err != nil {
		return err
	}

	httpRC := 0
	msg := ""
	httpRC, msg, _, err = r.storage.UpsertModel(importKey, buf.Bytes())
	if err != nil {
		return err
	}
	if httpRC != http.StatusCreated && httpRC != http.StatusOK {
		return fmt.Errorf("post to storage returned rc %d: %s", httpRC, msg)
	}
	return nil
}

func (r *RHOAINormalizerReconcile) processKFMR(ctx context.Context, name types.NamespacedName, is *serverapiv1beta1.InferenceService, bwriter io.Writer, log logr.Logger) (string, error) {
	ready := r.setupKFMR(ctx)
	if !ready {
		log.V(4).Info(fmt.Sprintf("reconciling inferenceservice %s, no kmr routes with ingress", name.String()))
		return "", nil
	}

	var kfmrRMs []openapi.RegisteredModel
	var kfmrISs []openapi.InferenceService
	for _, kfmr := range r.kfmr {
		rms, err := kfmr.ListRegisteredModels()
		if err != nil {
			log.Error(err, fmt.Sprintf("reconciling inferenceservice %s, error listing kfmr registered models", name.String()))
			// if we cannot fetch registered models, we won't bother with inference services
			continue
		}
		kfmrRMs = append(kfmrRMs, rms...)
		iss, err := kfmr.ListInferenceServices()
		if err != nil {
			log.Error(err, fmt.Sprintf("reconciling inferenceservice %s, error listing kfmr registered models", name.String()))
		}
		kfmrISs = append(kfmrISs, iss...)
		// if for some reason kserve/kubeflow reconciliation is not working and there are no kubeflow inference services,
		// let's match up based on registered model / model version name
		if len(kfmrISs) == 0 {
			for _, rm := range kfmrRMs {
				mvs := []openapi.ModelVersion{}
				mvs, err = kfmr.ListModelVersions(rm.GetId())
				if err != nil {
					log.Error(err, fmt.Sprintf("reconciling inferenceservice %s, error list kfmr model version %s", name.String(), rm.GetId()))
				}
				for _, mv := range mvs {
					if util.KServeInferenceServiceMapping(rm.Name, mv.Name, is.Name) {
						// let's go with this one
						var mas []openapi.ModelArtifact
						mas, err = kfmr.ListModelArtifacts(mv.GetId())
						if err != nil {
							log.Error(err, fmt.Sprintf("reconciling inferenceservice %s, error getting kfmr model artifacts for %s", name.String(), mv.GetId()))
							// don't just continue, try to build a catalog entry with the subset of info available
						}
						if mas == nil {
							log.Info(fmt.Sprintf("either mv %#v or mas %#v is nil, bypassing CallBackstagePrinters", mv, mas))
							continue
						}

						err = kubeflowmodelregistry.CallBackstagePrinters(ctx,
							r.defaultOwner,
							r.defaultLifecycle,
							&rm,
							//TODO deal with multiple versions
							[]openapi.ModelVersion{mv},
							map[string][]openapi.ModelArtifact{mv.GetId(): mas},
							[]openapi.InferenceService{},
							is,
							kfmr,
							r.client,
							bwriter,
							r.format)

						if err != nil {
							return "", err
						}

						//TODO iterate on the the REST URI's for our models if multi model?
						importKey, _ := util.BuildImportKeyAndURI(util.SanitizeName(rm.Name), util.SanitizeName(mv.Name), r.format)
						return importKey, nil
					}
				}
			}
		}

		for _, rm := range kfmrRMs {
			if rm.Id == nil {
				log.Info(fmt.Sprintf("reconciling inferenceservice %s, registered model %s has no ID", name.String(), rm.Name))
				continue
			}

			for _, kfmrIS := range kfmrISs {
				if kfmrIS.Id != nil && kfmrIS.RegisteredModelId == *rm.Id && strings.HasPrefix(kfmrIS.GetName(), is.Name) {
					seId := kfmrIS.GetServingEnvironmentId()
					var se *openapi.ServingEnvironment
					se, err = kfmr.GetServingEnvironment(seId)
					if err != nil {
						log.Error(err, fmt.Sprintf("reconciling inferenceservice %s, error getting kfmr serving environment %s", name.String(), seId))
						continue
					}

					if se.Name != nil && *se.Name == is.Namespace {
						// FOUND the match !!
						// reminder based on explanations about model artifact actually being the "root" of their model, and what has been observed in testing,
						mvId := kfmrIS.GetModelVersionId()
						var mas []openapi.ModelArtifact
						var mv *openapi.ModelVersion
						mv, err = kfmr.GetModelVersions(mvId)
						if err != nil {
							log.Error(err, fmt.Sprintf("reconciling inferenceservice %s, error getting kfmr model version %s", name.String(), mvId))
							// don't just continue, try to build a catalog entry with the subset of info available
						}
						mas, err = kfmr.ListModelArtifacts(mvId)
						if err != nil {
							log.Error(err, fmt.Sprintf("reconciling inferenceservice %s, error getting kfmr model artifacts for %s", name.String(), mvId))
							// don't just continue, try to build a catalog entry with the subset of info available
						}

						if mv == nil || mas == nil {
							log.Info(fmt.Sprintf("either mv %#v or mas %#v is nil, bypassing CallBackstagePrinters", mv, mas))
							continue
						}

						err = kubeflowmodelregistry.CallBackstagePrinters(ctx,
							r.defaultOwner,
							r.defaultLifecycle,
							&rm,
							//TODO deal with multiple versions
							[]openapi.ModelVersion{*mv},
							map[string][]openapi.ModelArtifact{mvId: mas},
							[]openapi.InferenceService{kfmrIS},
							is,
							kfmr,
							r.client,
							bwriter,
							r.format)

						if err != nil {
							return "", err
						}

						//TODO iterate on the the REST URI's for our models if multi model?
						importKey, _ := util.BuildImportKeyAndURI(util.SanitizeName(rm.Name), util.SanitizeName(mv.Name), r.format)
						return importKey, nil

					}

				}
			}
		}
	}

	// no match to kfmr, but do not return error, as caller can still process this as kserve only
	return "", nil
}

// Start - supplement with background polling as controller relist does not duplicate delete events, and we can be more
// fine grained on what we attempt to relist vs. just increasing the frequency of all the controller's watches

func (r *RHOAINormalizerReconcile) Start(ctx context.Context) error {
	eventTicker := time.NewTicker(r.pollingInt)
	for {
		select {
		case <-eventTicker.C:
			r.innerStart(ctx, nil, nil)

		case <-ctx.Done():
		}
	}
}

func (r *RHOAINormalizerReconcile) innerStart(ctx context.Context, buf *bytes.Buffer, bwriter *bufio.Writer) {
	r.setupKFMR(ctx)
	// we do not punt if there is no kfmr so as to handle the kserve only scenario

	keys := []string{}
	for _, kfmr := range r.kfmr {
		var err error
		var rms []openapi.RegisteredModel
		var mvs map[string][]openapi.ModelVersion
		var mas map[string]map[string][]openapi.ModelArtifact
		var isl []openapi.InferenceService

		rms, mvs, mas, err = kubeflowmodelregistry.LoopOverKFMR([]string{}, kfmr)
		if err != nil {
			controllerLog.Error(err, "err looping over KFMR")
			return
		}
		for _, rm := range rms {
			mva, ok := mvs[util.SanitizeName(rm.Name)]
			if !ok {
				continue
			}
			maa, ok2 := mas[util.SanitizeName(rm.Name)]
			if !ok2 {
				continue
			}
			for _, mv := range mva {

				importKey, _ := util.BuildImportKeyAndURI(util.SanitizeName(rm.Name), util.SanitizeName(mv.Name), r.format)
				keys = append(keys, importKey)
				eb := []byte{}
				ebuf := bytes.NewBuffer(eb)
				ewriter := bufio.NewWriter(ebuf)
				if buf != nil && bwriter != nil {
					ebuf = buf
					ewriter = bwriter
				}
				isl, err = kfmr.ListInferenceServices()
				if err != nil {
					controllerLog.Error(err, "error listing kubeflow inference services")
					continue
				}
				err = kubeflowmodelregistry.CallBackstagePrinters(ctx, r.defaultOwner, r.defaultLifecycle, &rm, mva, maa, isl, nil, kfmr, r.client, ewriter, r.format)
				if err != nil {
					controllerLog.Error(err, "error processing calling backstage printer")
					continue
				}
				err = r.processBWriter(ewriter, ebuf, importKey)
				if err != nil {
					controllerLog.Error(err, "error processing KFMR writer")
					continue
				}
			}
		}
	}

	isList := &serverapiv1beta1.InferenceServiceList{}
	listOptions := &client.ListOptions{Namespace: metav1.NamespaceAll}
	err := r.client.List(ctx, isList, listOptions)
	if err != nil {
		controllerLog.Error(err, "error listing kserve inferenceservices")
	}
	for _, is := range isList.Items {
		skip := false
		if is.Labels != nil {
			for k := range is.Labels {
				switch k {
				case bridgerest.INF_SVC_MV_ID_LABEL:
					fallthrough
				case bridgerest.INF_SVC_RM_ID_LABEL:
					controllerLog.V(4).Info(fmt.Sprintf("innerStart skipping inference service %s:%s since it is managed by kubeflow", is.Namespace, is.Name))
					skip = true
					break
				}
			}
		}
		if !skip {
			// we'll let the reconcile loop build the entry; let's just add the key for the current key set call
			importKey, _ := util.BuildImportKeyAndURI(util.SanitizeName(is.Namespace), util.SanitizeName(is.Name), r.format)
			keys = append(keys, importKey)
		}
	}

	rc := 0
	msg := ""
	rc, msg, err = r.storage.PostCurrentKeySet(keys)
	if err != nil {
		controllerLog.Error(err, "error updating current key set")
		return
	}
	if rc != http.StatusCreated && rc != http.StatusOK {
		controllerLog.Error(fmt.Errorf("post to storage returned rc %d: %s", rc, msg), "bad rc updating current key set")
	}

}
