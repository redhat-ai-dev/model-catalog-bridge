package kubeflowmodelregistry

import (
	"context"
	"fmt"
	serverv1beta1 "github.com/kserve/kserve/pkg/apis/serving/v1beta1"
	"github.com/kubeflow/model-registry/pkg/openapi"
	"github.com/redhat-ai-dev/model-catalog-bridge/pkg/cmd/cli/backstage"
	"github.com/redhat-ai-dev/model-catalog-bridge/pkg/cmd/cli/kserve"
	brdgtypes "github.com/redhat-ai-dev/model-catalog-bridge/pkg/types"
	"github.com/redhat-ai-dev/model-catalog-bridge/pkg/util"
	"github.com/redhat-ai-dev/model-catalog-bridge/schema/types/golang"
	"io"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"regexp"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (

	// pulled from makeValidator.ts in the catalog-model package in core backstage
	tagRegexp = "^[a-z0-9:+#]+(\\-[a-z0-9:+#]+)*$"
)

func LoopOverKFMR(owner, lifecycle string, ids []string, writer io.Writer, format brdgtypes.NormalizerFormat, kfmr *KubeFlowRESTClientWrapper, client client.Client) ([]openapi.RegisteredModel, map[string][]openapi.ModelVersion, error) {
	var err error
	var isl []openapi.InferenceService
	rmArray := []openapi.RegisteredModel{}
	mvsMap := map[string][]openapi.ModelVersion{}

	isl, err = kfmr.ListInferenceServices()

	if len(ids) == 0 {
		var rms []openapi.RegisteredModel
		rms, err = kfmr.ListRegisteredModels()
		if err != nil {
			klog.Errorf("list registered models error: %s", err.Error())
			klog.Flush()
			return nil, nil, err
		}
		for _, rm := range rms {
			if rm.State != nil && *rm.State == openapi.REGISTEREDMODELSTATE_ARCHIVED {
				klog.V(4).Infof("LoopOverKFMR skipping archived registered model %s", rm.Name)
				continue
			}
			var mvs []openapi.ModelVersion
			var mas map[string][]openapi.ModelArtifact
			mvs, mas, err = callKubeflowREST(*rm.Id, kfmr)
			if err != nil {
				klog.Errorf("%s", err.Error())
				klog.Flush()
				return nil, nil, err
			}
			err = CallBackstagePrinters(owner, lifecycle, &rm, mvs, mas, isl, nil, kfmr, client, writer, format)
			if err != nil {
				klog.Errorf("print model catalog: %s", err.Error())
				klog.Flush()
				return nil, nil, err
			}
			rmArray = append(rmArray, rm)
			mvsMap[rm.Name] = mvs
		}
	} else {
		for _, id := range ids {
			var rm *openapi.RegisteredModel
			rm, err = kfmr.GetRegisteredModel(id)
			if err != nil {
				klog.Errorf("get registered model error for %s: %s", id, err.Error())
				klog.Flush()
				return nil, nil, err
			}
			if rm.State != nil && *rm.State == openapi.REGISTEREDMODELSTATE_ARCHIVED {
				klog.V(4).Infof("LoopOverKFMR skipping archived registered model %s", rm.Name)
				continue
			}
			var mvs []openapi.ModelVersion
			var mas map[string][]openapi.ModelArtifact
			mvs, mas, err = callKubeflowREST(*rm.Id, kfmr)
			if err != nil {
				klog.Errorf("get model version/artifact error for %s: %s", id, err.Error())
				klog.Flush()
				return nil, nil, err
			}
			err = CallBackstagePrinters(owner, lifecycle, rm, mvs, mas, isl, nil, kfmr, client, writer, format)
			rmArray = append(rmArray, *rm)
			mvsMap[rm.Name] = mvs
		}
	}
	return rmArray, mvsMap, nil
}

func callKubeflowREST(id string, kfmr *KubeFlowRESTClientWrapper) (mvs []openapi.ModelVersion, ma map[string][]openapi.ModelArtifact, err error) {
	mvs, err = kfmr.ListModelVersions(id)
	if err != nil {
		klog.Errorf("ERROR: error list model versions for %s: %s", id, err.Error())
		return
	}
	ma = map[string][]openapi.ModelArtifact{}
	for _, mv := range mvs {
		if mv.State != nil && *mv.State == openapi.MODELVERSIONSTATE_ARCHIVED {
			klog.V(4).Infof("callKubeflowREST skipping archived model version %s", mv.Name)
			continue
		}
		var v []openapi.ModelArtifact
		v, err = kfmr.ListModelArtifacts(*mv.Id)
		if err != nil {
			klog.Errorf("ERROR error list model artifacts for %s:%s: %s", id, *mv.Id, err.Error())
			return
		}
		if len(v) == 0 {
			v, err = kfmr.ListModelArtifacts(id)
			if err != nil {
				klog.Errorf("ERROR error list model artifacts for %s:%s: %s", id, *mv.Id, err.Error())
				return
			}
		}
		ma[*mv.Id] = v
	}
	return
}

// json array schema populator

type CommonSchemaPopulator struct {
	// reuse the component populator as it houses all the KFMR artifacts of noew
	ComponentPopulator
}

type ModelCatalogPopulator struct {
	CommonSchemaPopulator
	MSPop *ModelServerPopulator
	MPops []*ModelPopulator
}

func (m *ModelCatalogPopulator) GetModels() []golang.Model {
	models := []golang.Model{}
	for mvidx, mv := range m.ModelVersions {
		mPop := m.MPops[mvidx]
		mPop.MVIndex = mvidx
		mas := m.ModelArtifacts[mv.GetId()]
		for maidx, ma := range mas {
			if ma.GetId() == m.RegisteredModel.GetId() {
				mPop.MAIndex = maidx
				break
			}
		}

		model := golang.Model{
			ArtifactLocationURL: mPop.GetArtifactLocationURL(),
			Description:         mPop.GetDescription(),
			Ethics:              mPop.GetEthics(),
			HowToUseURL:         mPop.GetHowToUseURL(),
			Lifecycle:           mPop.Lifecycle,
			Name:                mPop.GetName(),
			Owner:               mPop.GetOwner(),
			Support:             mPop.GetSupport(),
			Tags:                mPop.GetTags(),
			Training:            mPop.GetTraining(),
			Usage:               mPop.GetUsage(),
		}
		models = append(models, model)
	}
	return models
}

func (m *ModelCatalogPopulator) GetModelServer() *golang.ModelServer {
	infSvcIdx := 0
	mvIndex := 0
	maIndex := 0

	kfmrIS := openapi.InferenceService{}
	for isidx, is := range m.InferenceServices {
		if is.RegisteredModelId == m.RegisteredModel.GetId() {
			infSvcIdx = isidx
			kfmrIS = is
			break
		}
	}

	mas := []openapi.ModelArtifact{}
	for mvidx, mv := range m.ModelVersions {
		if mv.RegisteredModelId == m.RegisteredModel.GetId() && mv.GetId() == kfmrIS.GetModelVersionId() {
			mvIndex = mvidx
			mas = m.ModelArtifacts[mv.GetId()]
			break
		}
	}

	// reminder based on explanations about model artifact actually being the "root" of their model, and what has been observed in testing,
	// the ID for the registered model and model artifact appear to match
	maId := m.RegisteredModel.GetId()
	for maidx, ma := range mas {
		if ma.GetId() == maId {
			maIndex = maidx
			break
		}
	}

	m.MSPop.InfSvcIndex = infSvcIdx
	m.MSPop.MVIndex = mvIndex
	m.MSPop.MAIndex = maIndex

	return &golang.ModelServer{
		API:            m.MSPop.GetAPI(),
		Authentication: m.MSPop.GetAuthentication(),
		Description:    m.MSPop.GetDescription(),
		HomepageURL:    m.MSPop.GetHomepageURL(),
		Lifecycle:      m.MSPop.GetLifecycle(),
		Name:           m.MSPop.GetName(),
		Owner:          m.MSPop.GetOwner(),
		Tags:           m.MSPop.GetTags(),
		Usage:          m.MSPop.GetUsage(),
	}

	return nil
}

type ModelPopulator struct {
	CommonSchemaPopulator
	MVIndex int
	MAIndex int
}

func (m *ModelPopulator) GetName() string {
	if len(m.ModelVersions) > m.MVIndex {
		mv := m.ModelVersions[m.MVIndex]
		return mv.GetName()
	}
	return ""
}

func (m *ModelPopulator) GetOwner() string {
	//TODO need to specify a well known k/v pair or env var for default
	return util.DefaultOwner
}

func (m *ModelPopulator) GetLifecycle() string {
	//TODO need to specify a well known k/v pair or env var for default
	return util.DefaultLifecycle
}

func (m *ModelPopulator) GetDescription() string {
	if len(m.ModelVersions) > m.MVIndex {
		mv := m.ModelVersions[m.MVIndex]
		return mv.GetDescription()
	}
	return ""
}

func (m *ModelPopulator) GetTags() []string {
	tags := []string{}
	if len(m.ModelVersions) > m.MVIndex {
		mv := m.ModelVersions[m.MVIndex]
		if mv.HasCustomProperties() {
			for cpk := range mv.GetCustomProperties() {
				tags = append(tags, cpk)
			}
		}
		mas, ok := m.ModelArtifacts[mv.Name]
		if ok {
			ma := mas[m.MAIndex]
			if ma.HasCustomProperties() {
				for cpk := range ma.GetCustomProperties() {
					tags = append(tags, cpk)
				}
			}
		}
	}
	return tags
}

func (m *ModelPopulator) GetArtifactLocationURL() *string {
	if len(m.ModelVersions) > m.MVIndex {
		mv := m.ModelVersions[m.MVIndex]
		mas, ok := m.ModelArtifacts[mv.GetId()]
		if ok {
			if len(mas) > m.MAIndex {
				ma := mas[m.MAIndex]
				return ma.Uri
			}
		}
	}
	return nil
}

func (m *ModelPopulator) GetEthics() *string {
	//TODO need to specify a well known k/v pair
	return nil
}

func (m *ModelPopulator) GetHowToUseURL() *string {
	//TODO need to specify a well known k/v pair
	return nil
}

func (m *ModelPopulator) GetSupport() *string {
	//TODO need to specify a well known k/v pair
	return nil
}

func (m *ModelPopulator) GetTraining() *string {
	//TODO need to specify a well known k/v pair
	return nil
}

func (m *ModelPopulator) GetUsage() *string {
	//TODO need to specify a well known k/v pair
	return nil
}

type ModelServerPopulator struct {
	CommonSchemaPopulator
	ApiPop      ModelServerAPIPopulator
	InfSvcIndex int
	MVIndex     int
	MAIndex     int
}

func (m *ModelServerPopulator) GetUsage() *string {
	// unless say the ModelCard output can be retrievable somehow ...
	//TODO need to specify a well known k/v pair
	return nil
}

func (m *ModelServerPopulator) GetHomepageURL() *string {
	//TODO need to specify a well known k/v pair
	return nil
}

func (m *ModelServerPopulator) GetAuthentication() *bool {
	auth := false
	//TODO have not been able to figure out where the setting of "auth needed" when deploying a model from the MR in the UI gets stored in the plethora
	// of MR / K8s data types around model serving .... need to ask the RHOAI folks ... maybe it is related to the tls termination policy on the Route?
	return &auth
}

func (m *ModelServerPopulator) GetName() string {
	if len(m.InferenceServices) > m.InfSvcIndex {
		return m.InferenceServices[m.InfSvcIndex].GetName()
	}
	return ""
}

func (m *ModelServerPopulator) GetTags() []string {
	tags := m.ApiPop.GetTags()
	if len(m.ModelVersions) > m.MVIndex {
		mv := m.ModelVersions[m.MVIndex]
		if mv.HasCustomProperties() {
			for cpk := range mv.GetCustomProperties() {
				tags = append(tags, cpk)
			}
		}
		mas, ok := m.ModelArtifacts[mv.Name]
		if ok {
			if len(mas) > m.MAIndex {
				ma := mas[m.MAIndex]
				if ma.HasCustomProperties() {
					for cpk := range ma.GetCustomProperties() {
						tags = append(tags, cpk)
					}
				}

			}
		}
	}
	return tags
}

func (m *ModelServerPopulator) GetAPI() *golang.API {
	api := &golang.API{
		Spec: m.ApiPop.GetSpec(),
		Tags: m.ApiPop.GetTags(),
		Type: m.ApiPop.GetType(),
		URL:  m.ApiPop.GetURL(),
	}
	return api
}

func (m *ModelServerPopulator) GetOwner() string {
	//TODO need to specify a well known k/v pair or env var for default
	return util.DefaultOwner
}

func (m *ModelServerPopulator) GetLifecycle() string {
	//TODO need to specify a well known k/v pair or env var for default
	return util.DefaultLifecycle
}

func (m *ModelServerPopulator) GetDescription() string {
	return m.RegisteredModel.GetDescription()
}

type ModelServerAPIPopulator struct {
	CommonSchemaPopulator
}

func (m *ModelServerAPIPopulator) GetSpec() string {
	//TODO need to specify a well known k/v pair
	return ""
}

func (m *ModelServerAPIPopulator) GetTags() []string {
	tags := []string{}
	regex, _ := regexp.Compile(tagRegexp)
	if m.RegisteredModel.CustomProperties != nil {
		for cPropKey := range *m.RegisteredModel.CustomProperties {
			if regex.MatchString(cPropKey) && len(cPropKey) <= 63 {
				tags = append(tags, cPropKey)
				continue
			}
			klog.Infof("skipping custom prop key %s", cPropKey)
		}
	}
	return tags
}

func (m *ModelServerAPIPopulator) GetType() golang.Type {
	//TODO need to specify a well known k/v pair
	return golang.Openapi
}

func (m *ModelServerAPIPopulator) GetURL() string {
	if m.Kis == nil {
		m.getLinksFromInferenceServices()
	}
	if m.Kis != nil && m.Kis.Status.URL != nil && m.Kis.Status.URL.URL() != nil {
		// return the KServe InferenceService Route URL
		return m.Kis.Status.URL.URL().String()
	}

	return ""
}

// catalog-info.yaml populators

func CallBackstagePrinters(owner, lifecycle string, rm *openapi.RegisteredModel, mvs []openapi.ModelVersion, mas map[string][]openapi.ModelArtifact, isl []openapi.InferenceService, is *serverv1beta1.InferenceService, kfmr *KubeFlowRESTClientWrapper, client client.Client, writer io.Writer, format brdgtypes.NormalizerFormat) error {
	compPop := ComponentPopulator{}
	compPop.Owner = owner
	compPop.Lifecycle = lifecycle
	compPop.Kfmr = kfmr
	compPop.RegisteredModel = rm
	compPop.ModelVersions = mvs
	compPop.ModelArtifacts = mas
	compPop.InferenceServices = isl
	compPop.Kis = is
	compPop.CtrlClient = client

	switch format {
	case brdgtypes.JsonArrayForamt:
		mcPop := ModelCatalogPopulator{CommonSchemaPopulator: CommonSchemaPopulator{compPop}}
		msPop := ModelServerPopulator{
			CommonSchemaPopulator: CommonSchemaPopulator{compPop},
			ApiPop:                ModelServerAPIPopulator{CommonSchemaPopulator: CommonSchemaPopulator{compPop}},
		}
		mcPop.MSPop = &msPop
		mPop := ModelPopulator{CommonSchemaPopulator: CommonSchemaPopulator{compPop}}
		mcPop.MPops = []*ModelPopulator{&mPop}
		return backstage.PrintModelCatalogPopulator(&mcPop, writer)
	case brdgtypes.CatalogInfoYamlFormat:
		fallthrough
	default:
		err := backstage.PrintComponent(&compPop, writer)
		if err != nil {
			return err
		}

		resPop := ResourcePopulator{}
		resPop.Owner = owner
		resPop.Lifecycle = lifecycle
		resPop.Kfmr = kfmr
		resPop.RegisteredModel = rm
		resPop.Kis = is
		resPop.CtrlClient = client
		for _, mv := range mvs {
			resPop.ModelVersion = &mv
			m, _ := mas[*mv.Id]
			resPop.ModelArtifacts = m
			err = backstage.PrintResource(&resPop, writer)
			if err != nil {
				return err
			}
		}

		apiPop := ApiPopulator{}
		apiPop.Owner = owner
		apiPop.Lifecycle = lifecycle
		apiPop.Kfmr = kfmr
		apiPop.RegisteredModel = rm
		apiPop.InferenceServices = isl
		apiPop.Kis = is
		apiPop.CtrlClient = client
		return backstage.PrintAPI(&apiPop, writer)
	}

	return nil

}

type CommonPopulator struct {
	Owner             string
	Lifecycle         string
	RegisteredModel   *openapi.RegisteredModel
	InferenceServices []openapi.InferenceService
	Kfmr              *KubeFlowRESTClientWrapper
	Kis               *serverv1beta1.InferenceService
	CtrlClient        client.Client
}

func (pop *CommonPopulator) GetOwner() string {
	if pop.RegisteredModel.Owner != nil {
		return *pop.RegisteredModel.Owner
	}
	return pop.Owner
}

func (pop *CommonPopulator) GetLifecycle() string {
	return pop.Lifecycle
}

func (pop *CommonPopulator) GetDescription() string {
	if pop.RegisteredModel.Description != nil {
		return *pop.RegisteredModel.Description
	}
	return ""
}

func (pop *CommonPopulator) GetProvidedAPIs() []string {
	return []string{}
}

type ComponentPopulator struct {
	CommonPopulator
	ModelVersions  []openapi.ModelVersion
	ModelArtifacts map[string][]openapi.ModelArtifact
}

func (pop *ComponentPopulator) GetName() string {
	return pop.RegisteredModel.Name
}

func (pop *ComponentPopulator) GetLinks() []backstage.EntityLink {
	links := pop.getLinksFromInferenceServices()
	// GGM maybe multi resource / multi model indication
	for _, maa := range pop.ModelArtifacts {
		for _, ma := range maa {
			if ma.Uri != nil {
				links = append(links, backstage.EntityLink{
					URL:   *ma.Uri,
					Title: ma.GetDescription(),
					Icon:  backstage.LINK_ICON_WEBASSET,
					Type:  backstage.LINK_TYPE_WEBSITE,
				})
			}
		}
	}

	return links
}

func (pop *CommonPopulator) getLinksFromInferenceServices() []backstage.EntityLink {
	links := []backstage.EntityLink{}
	for _, is := range pop.InferenceServices {
		var rmid *string
		var ok bool
		rmid, ok = pop.RegisteredModel.GetIdOk()
		if !ok {
			continue
		}
		if is.RegisteredModelId != *rmid {
			continue
		}
		var iss *openapi.InferenceServiceState
		iss, ok = is.GetDesiredStateOk()
		if !ok {
			continue
		}
		if *iss != openapi.INFERENCESERVICESTATE_DEPLOYED {
			continue
		}
		se, err := pop.Kfmr.GetServingEnvironment(is.ServingEnvironmentId)
		if err != nil {
			klog.Errorf("ComponentPopulator GetLinks: %s", err.Error())
			continue
		}
		if pop.Kis == nil {
			kisns := se.GetName()
			kisnm := is.GetRuntime()
			var kis *serverv1beta1.InferenceService
			if pop.Kfmr != nil && pop.Kfmr.Config != nil && pop.Kfmr.Config.ServingClient != nil {
				kis, err = pop.Kfmr.Config.ServingClient.InferenceServices(kisns).Get(context.Background(), kisnm, metav1.GetOptions{})
			}
			if kis == nil && pop.CtrlClient != nil {
				kis = &serverv1beta1.InferenceService{}
				err = pop.CtrlClient.Get(context.Background(), types.NamespacedName{Namespace: kisns, Name: kisnm}, kis)
			}

			if err != nil {
				klog.Errorf("ComponentPopulator GetLinks: %s", err.Error())
				continue
			}
			pop.Kis = kis
		}
		kpop := kserve.CommonPopulator{InferSvc: pop.Kis}
		links = append(links, kpop.GetLinks()...)
	}
	return links
}

func (pop *ComponentPopulator) GetTags() []string {
	tags := []string{}
	regex, _ := regexp.Compile(tagRegexp)
	for key, value := range pop.RegisteredModel.GetCustomProperties() {
		if !regex.MatchString(key) {
			klog.Infof("skipping custom prop %s for tags", key)
			continue
		}
		tag := key
		if value.MetadataStringValue != nil {
			strVal := value.MetadataStringValue.StringValue
			if !regex.MatchString(fmt.Sprintf("%v", strVal)) {
				klog.Infof("skipping custom prop value %v for tags", value.GetActualInstance())
				continue
			}
			tag = fmt.Sprintf("%s-%s", tag, strVal)
		}

		if len(tag) > 63 {
			klog.Infof("skipping tag %s because its length is greater than 63", tag)
		}

		tags = append(tags, tag)
	}

	return tags
}

func (pop *ComponentPopulator) GetDependsOn() []string {
	depends := []string{}
	for _, mv := range pop.ModelVersions {
		depends = append(depends, "resource:"+mv.Name)
	}
	for _, mas := range pop.ModelArtifacts {
		for _, ma := range mas {
			depends = append(depends, "api:"+*ma.Name)
		}
	}
	return depends
}

func (pop *ComponentPopulator) GetTechdocRef() string {
	return "./"
}

func (pop *ComponentPopulator) GetDisplayName() string {
	return pop.GetName()
}

type ResourcePopulator struct {
	CommonPopulator
	ModelVersion   *openapi.ModelVersion
	ModelArtifacts []openapi.ModelArtifact
}

func (pop *ResourcePopulator) GetName() string {
	return pop.ModelVersion.Name
}

func (pop *ResourcePopulator) GetTechdocRef() string {
	return "resource/"
}

func (pop *ResourcePopulator) GetLinks() []backstage.EntityLink {
	links := []backstage.EntityLink{}
	// GGM maybe multi resource / multi model indication
	for _, ma := range pop.ModelArtifacts {
		if ma.Uri != nil {
			links = append(links, backstage.EntityLink{
				URL:   *ma.Uri,
				Title: ma.GetDescription(),
				Icon:  backstage.LINK_ICON_WEBASSET,
				Type:  backstage.LINK_TYPE_WEBSITE,
			})
		}
	}
	return links
}

func (pop *ResourcePopulator) GetTags() []string {
	tags := []string{}
	regex, _ := regexp.Compile(tagRegexp)
	for key, value := range pop.ModelVersion.GetCustomProperties() {
		if !regex.MatchString(key) {
			klog.Infof("skipping custom prop %s for tags", key)
			continue
		}
		tag := key
		if value.MetadataStringValue != nil {
			strVal := value.MetadataStringValue.StringValue
			if !regex.MatchString(fmt.Sprintf("%v", strVal)) {
				klog.Infof("skipping custom prop value %v for tags", value.GetActualInstance())
				continue
			}
			tag = fmt.Sprintf("%s-%s", tag, strVal)
		}
		if len(tag) > 63 {
			klog.Infof("skipping tag %s because its length is greater than 63", tag)
		}

		tags = append(tags, tag)
	}

	for _, ma := range pop.ModelArtifacts {
		for k, v := range ma.GetCustomProperties() {
			if !regex.MatchString(k) {
				klog.Infof("skipping custom prop %s for tags", k)
				continue
			}
			tag := k
			if v.MetadataStringValue != nil {
				strVal := v.MetadataStringValue.StringValue
				if !regex.MatchString(fmt.Sprintf("%v", strVal)) {
					klog.Infof("skipping custom prop value %v for tags", v.GetActualInstance())
					continue
				}
				tag = fmt.Sprintf("%s-%s", tag, strVal)
			}

			if len(tag) > 63 {
				klog.Infof("skipping tag %s because its length is greater than 63", tag)
			}

			tags = append(tags, tag)
		}
	}
	return tags
}

func (pop *ResourcePopulator) GetDependencyOf() []string {
	return []string{fmt.Sprintf("component:%s", pop.RegisteredModel.Name)}
}

func (pop *ResourcePopulator) GetDisplayName() string {
	return pop.GetName()
}

type ApiPopulator struct {
	CommonPopulator
}

func (pop *ApiPopulator) GetName() string {
	return pop.RegisteredModel.Name
}

func (pop *ApiPopulator) GetDependencyOf() []string {
	return []string{fmt.Sprintf("component:%s", pop.RegisteredModel.Name)}
}

func (pop *ApiPopulator) GetDefinition() string {
	// definition must be set to something to pass backstage validation
	return "no-definition-yet"
}

func (pop *ApiPopulator) GetTechdocRef() string {
	// TODO in theory the Kfmr modelcard support when it arrives will replace this
	return "api/"
}

func (pop *ApiPopulator) GetTags() []string {
	return []string{}
}

func (pop *ApiPopulator) GetLinks() []backstage.EntityLink {
	return pop.getLinksFromInferenceServices()
}

func (pop *ApiPopulator) GetDisplayName() string {
	return pop.GetName()
}
