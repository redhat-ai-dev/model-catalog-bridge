package storage

import (
	"encoding/json"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/redhat-ai-dev/model-catalog-bridge/pkg/cmd/cli/backstage"
	bridgeclient "github.com/redhat-ai-dev/model-catalog-bridge/pkg/cmd/server/location/client"
	"github.com/redhat-ai-dev/model-catalog-bridge/pkg/config"
	"github.com/redhat-ai-dev/model-catalog-bridge/pkg/rest"
	"github.com/redhat-ai-dev/model-catalog-bridge/pkg/types"
	"github.com/redhat-ai-dev/model-catalog-bridge/pkg/util"
	"k8s.io/klog/v2"
	"net/http"
	"strings"
	"sync"
)

type StorageRESTServer struct {
	router          *gin.Engine
	st              types.BridgeStorage
	mutex           sync.Mutex
	pushedLocations map[string]*types.StorageBody
	locations       *bridgeclient.BridgeLocationRESTClient
	bkstg           rest.BackstageImport
	format          types.NormalizerFormat
}

func NewStorageRESTServer(st types.BridgeStorage, bridgeURL, bridgeToken, bkstgURL, bkstgToken string, nf types.NormalizerFormat) *StorageRESTServer {
	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()
	cfg := &config.Config{
		BackstageURL:     bkstgURL,
		BackstageToken:   bkstgToken,
		BackstageSkipTLS: true,
	}
	s := &StorageRESTServer{
		router:          r,
		st:              st,
		mutex:           sync.Mutex{},
		pushedLocations: map[string]*types.StorageBody{},
		locations:       bridgeclient.SetupBridgeLocationRESTClient(bridgeURL, bridgeToken),
		bkstg:           backstage.SetupBackstageRESTClient(cfg),
		format:          nf,
	}
	klog.Infof("NewStorageRESTServer")
	r.SetTrustedProxies(nil)
	r.TrustedPlatform = "X-Forwarded-For"
	r.Use(addRequestId())
	r.POST(util.UpsertURI, s.handleCatalogUpsertPost)
	r.POST(util.CurrentKeySetURI, s.handleCatalogCurrentKeySetPost)
	r.GET(util.ListURI, s.handleCatalogList)
	r.GET(util.FetchURI, s.handleCatalogFetch)
	return s
}

// Middleware adding request ID to gin context.
// Note that this is a simple unique ID that can be used for debugging purposes.
// In the future, this might be replaced with OpenTelemetry IDs/tooling.
func addRequestId() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set("requestId", uuid.New().String())
		c.Next()
	}
}

func (s *StorageRESTServer) Run(stopCh <-chan struct{}) {
	ch := make(chan int)
	go func() {
		for {
			select {
			case <-ch:
				return
			default:
				err := s.router.Run(":7070")
				if err != nil {
					klog.Errorf("ERROR: gin-gonic run error %s", err.Error())
				}
			}
		}
	}()
	<-stopCh
	close(ch)
}

func (s *StorageRESTServer) sync(key string) (*types.StorageBody, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	sb, ok := s.pushedLocations[key]
	if ok && sb.LocationIDValid {
		return sb, nil
	}
	ssb, err := s.st.Fetch(key)
	if err == nil && len(ssb.LocationId) > 0 {
		_, err = s.bkstg.GetLocation(ssb.LocationId)
		if err == nil {
			ssb.LocationIDValid = true
			s.pushedLocations[key] = &ssb
			//TODO do we bother updating the storage tier
		} else {
			klog.Infof("previously registered location %s:%s is no longer valid, unregistering", ssb.LocationId, ssb.LocationTarget)
			delete(s.pushedLocations, key)
			return &types.StorageBody{}, s.st.Remove(key)
		}
	}
	return &ssb, err
}

func (s *StorageRESTServer) del(key string) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	delete(s.pushedLocations, key)
}

// handleCatalogCurrentKeySetPost deals with removing model/version entries no longer recognized by our set
// of metadata normalizers.  It pulls the list of keys in storage and if any of those keys in storage are not
// in the current key set provided as input, removal processing is initiated.  That removal processing includes:
//   - fetching the storage entry
//   - remove the storage entry
//   - based on the last version of the storage entry retrieved, if it has the location ID set, that means it was
//     imported to backstage; at this time, we'll explicitly delete via the Backstage Catalog REST API; NOTE: we originally
//     considered letting the EntityProvider for the bridge detect removed locations with its polling of the location service and
//     deleting the location and its related components/resources/apis from the catalog (i.e. a less
//     aggressive delete) but for a TBD reason deleting the location does not appear to be working form our EntityProvider
//   - we then remove the entry from the location service
func (s *StorageRESTServer) handleCatalogCurrentKeySetPost(c *gin.Context) {
	key := c.Query("key")
	// no content for the key QP means no models were discovered

	keys := strings.Split(key, ",")
	keyHash := map[string]struct{}{}
	if len(key) > 0 {
		for _, k := range keys {
			keyHash[k] = struct{}{}
		}
	}

	var err error
	currentKeys := []string{}
	currentKeys, err = s.st.List()
	if err != nil {
		c.Status(http.StatusInternalServerError)
		msg := fmt.Sprintf("error listing location keys: %s", err.Error())
		klog.Errorf(msg)
		c.Error(fmt.Errorf(msg))
		return
	}

	var errors []error
	for _, k := range currentKeys {
		_, ok := keyHash[k]
		if !ok {
			msg := ""
			//TODO for summit we were not going to "aggressively" inform backstage of deletions by leveraging
			// the delete location catalog REST API; however, with local testing
			// https://github.com/redhat-ai-dev/rhdh-plugins/blob/6b0c4a21c1cdfeba4cf2618d4aabadff544c7efc/workspaces/rhdh-ai/plugins/catalog-backend-module-rhdh-ai/src/providers/RHDHRHOAIEntityProvider.ts#L198-L202
			// is not actually deleting locations as expected.  So we are provisionally (we'll see if it is permananent after diagnosing the situation) using the catalog REST API to delete for now.
			sb := types.StorageBody{}
			sb, err = s.st.Fetch(k)
			if err != nil {
				// just log error for now
				klog.Error(err.Error())
			}

			// initiate removal
			err = s.st.Remove(k)
			if err != nil {
				klog.Errorf(fmt.Sprintf("error removing from storage key %s: %s", k, err.Error()))
				errors = append(errors, err)
				continue
			}

			s.del(k)
			//TODO provisional direct delete of location
			if len(sb.LocationId) > 0 {
				msg, err = s.bkstg.DeleteLocation(sb.LocationId)
				if err == nil {
					klog.Infof("deletion of location %s for target %s successful", sb.LocationId, sb.LocationTarget)
				} else {
					klog.Errorf("deletions of location %s for target %s had error %s: %s", sb.LocationId, sb.LocationTarget, msg, err.Error())
				}
			}

			rc := 0
			rc, msg, err = s.locations.RemoveModel(k)
			if err != nil {
				errors = append(errors, err)
				continue
			}
			if rc != http.StatusOK && rc != http.StatusCreated {
				err = fmt.Errorf("bad rc removing from storage key %d: %s", rc, msg)
				klog.Errorf(err.Error())
				errors = append(errors, err)
				continue
			}
		}
	}

	if len(errors) > 0 {
		c.Status(http.StatusInternalServerError)
		msg := ""
		for _, e := range errors {
			msg = fmt.Sprintf("%s;%s", msg, e.Error())
		}
		c.Error(fmt.Errorf(msg))
		return
	}
	c.Status(http.StatusOK)
}

// handleCatalogUpsertPost deals with either creating or updating new model content in storage, as well as coordinating
// that content with the location service and backstage.  It pulls the key from the query parameter and then
//   - fetches the entry if exists in storage, populating our cache and syncing via a golang mutex to make the operation atomic,
//     regardless if the backend store is transactional
//   - stores the latest data for the new key in storage
//   - updates the location service with the corresponding URI and content
//   - if importing to backstage was not previously done, it does that, and then stores the ID returned form backstage in storage
func (s *StorageRESTServer) handleCatalogUpsertPost(c *gin.Context) {
	key := c.Query("key")
	if len(key) == 0 {
		c.Status(http.StatusBadRequest)
		c.Error(fmt.Errorf("need a 'key' parameter"))
		return
	}
	var postBody rest.PostBody
	err := c.BindJSON(&postBody)
	if err != nil {
		c.Status(http.StatusBadRequest)
		msg := fmt.Sprintf("error reading POST body: %s", err.Error())
		klog.Errorf(msg)
		c.Error(fmt.Errorf(msg))
		return
	}
	//TOOD soon will have the type of normalizer preface the model name and version
	segs := strings.Split(key, "_")
	if len(segs) < 2 {
		c.Status(http.StatusBadRequest)
		c.Error(fmt.Errorf("bad key format: %s", key))
		return
	}
	uri := ""
	key, uri = util.BuildImportKeyAndURI(segs[0], segs[1], s.format)
	klog.Infof("Upserting URI %s with key %s with data of len %d", uri, key, len(postBody.Body))

	sb := &types.StorageBody{}
	sb, err = s.sync(key)
	if err != nil {
		klog.Error(err.Error())
		c.Status(http.StatusInternalServerError)
		return
	}

	alreadyPushed := len(sb.LocationId) > 0
	sb.Body = postBody.Body
	err = s.st.Upsert(key, *sb)
	if err != nil {
		c.Status(http.StatusInternalServerError)
		msg := fmt.Sprintf("error upserting to storage key %s POST body: %s", key, err.Error())
		klog.Errorf(msg)
		c.Error(fmt.Errorf(msg))
		return
	}

	// push update to bridge locations REST endpoint
	var rc int
	var msg string
	rc, msg, _, err = s.locations.UpsertModel(key, postBody.Body)
	if err != nil {
		c.Status(http.StatusInternalServerError)
		msg = fmt.Sprintf("error upserting to bridge uri %s POST body: msg %s error %s", uri, msg, err.Error())
		klog.Errorf(msg)
		c.Error(fmt.Errorf(msg))
		return
	}
	if rc != http.StatusCreated && rc != http.StatusOK {
		c.Status(rc)
		msg = fmt.Sprintf("error upserting to bridge uri %s POST body: msg %s", uri, msg)
		klog.Errorf(msg)
		c.Error(fmt.Errorf(msg))
	}

	// if we have not previously pushed to backstage, do so now;
	// we use a sync map here in case our store implementation does not provide atomic updates
	if alreadyPushed {
		klog.Info(fmt.Sprintf("%s already provides location %s", s.locations.UpsertURL, uri))
		c.Status(http.StatusOK)
		return
	}

	impResp := map[string]any{}
	impResp, err = s.bkstg.ImportLocation(s.locations.HostURL + uri)
	if err != nil {
		c.Status(http.StatusInternalServerError)
		msg = fmt.Sprintf("error importing location %s to backstage: %s", s.locations.HostURL+uri, err.Error())
		klog.Errorf(msg)
		c.Error(fmt.Errorf(msg))
		return
	}
	retID, retTarget, rok := rest.ParseImportLocationMap(impResp)
	if !rok {
		//TODO perhaps delete location on the backstage side as well as our cache
		c.Status(http.StatusBadRequest)
		msg = fmt.Sprintf("parsing of import location return had an issue: %#v", impResp)
		klog.Errorf(msg)
		c.Error(fmt.Errorf(msg))
		return
	}

	// finally store in our storage layer with the id and cross reference location URL from backstage
	sb.LocationId = retID
	sb.LocationTarget = retTarget
	err = s.st.Upsert(key, *sb)
	if err != nil {
		c.Status(http.StatusInternalServerError)
		//TODO perhaps delete location on the backstage side as well as our cache
		msg = fmt.Sprintf("error upserting to storage key %s POST body plus backstage ID: %s", key, err.Error())
		klog.Errorf(msg)
		c.Error(fmt.Errorf(msg))
		return
	}

	c.Status(http.StatusCreated)
}

type DiscoverResponse struct {
	Keys []string `json:"keys"`
}

func (s *StorageRESTServer) handleCatalogList(c *gin.Context) {
	var err error
	d := &DiscoverResponse{}
	d.Keys, err = s.st.List()
	if err != nil {
		c.Status(http.StatusInternalServerError)
		msg := fmt.Sprintf("error listing location keys: %s", err.Error())
		klog.Errorf(msg)
		c.Error(fmt.Errorf(msg))
		return
	}
	var content []byte
	content, err = json.Marshal(d)
	if err != nil {
		c.Status(http.StatusInternalServerError)
		c.Error(err)
		return
	}
	c.Data(http.StatusOK, "Content-Type: application/json", content)
}

func (s *StorageRESTServer) handleCatalogFetch(c *gin.Context) {
	key := c.Query("key")
	if len(key) == 0 {
		c.Status(http.StatusBadRequest)
		c.Error(fmt.Errorf("need a 'key' parameter"))
		return
	}
	var err error
	sb := types.StorageBody{}
	sb, err = s.st.Fetch(key)
	var content []byte
	content, err = json.Marshal(sb)
	if err != nil {
		c.Status(http.StatusInternalServerError)
		c.Error(err)
		return
	}
	c.Data(http.StatusOK, "Content-Type: application/json", content)
}
