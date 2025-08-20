package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/redhat-ai-dev/model-catalog-bridge/pkg/cmd/server/storage"
	"github.com/redhat-ai-dev/model-catalog-bridge/pkg/config"
	"github.com/redhat-ai-dev/model-catalog-bridge/pkg/rest"
	"github.com/redhat-ai-dev/model-catalog-bridge/pkg/types"
	"github.com/redhat-ai-dev/model-catalog-bridge/pkg/util"
	"k8s.io/klog/v2"
)

type ImportLocationServer struct {
	router     *gin.Engine
	content    map[string]*ImportLocation
	modelcards map[string]string
	storage    *storage.BridgeStorageRESTClient
	format     types.NormalizerFormat
	port       string
	lock       sync.Mutex
}

func NewImportLocationServer(stURL, port string, nf types.NormalizerFormat) *ImportLocationServer {
	//var content map[string]*ImportLocation
	gin.SetMode(gin.ReleaseMode)
	cfg, _ := util.GetK8sConfig(&config.Config{})
	r := gin.Default()
	i := &ImportLocationServer{
		router:  r,
		content: map[string]*ImportLocation{},
        modelcards: map[string]string{},
		storage: storage.SetupBridgeStorageRESTClient(stURL, util.GetCurrentToken(cfg)),
		format:  nf,
		port:    port,
		lock:    sync.Mutex{},
	}
	r.SetTrustedProxies(nil)
	r.TrustedPlatform = "X-Forwarded-For"
	r.Use(addRequestId())

	// approach for implementing background processing with gin gonic discovered via some AI interaction lead to some
	// timing issues with the periodic reconcile of the normalizer/storage-rest loop; decided not to start including
	// the synchronization needed to sort that out.
	// So instead, just doing a one time load attempt before registering the upsert handler to speed up location service
	// population before 2 minute poll interval the loading of reconciled models form storage

	klog.Info("one time load attempt from storage instead of waiting for the reconciliation loop")
	i.loadFromStorage()

	klog.Infof("NewImportLocationServer content len %d", len(i.content))
	r.GET(util.ListURI, i.handleCatalogDiscoveryGet)
	r.POST(util.UpsertURI, i.handleCatalogUpsertPost)
	r.DELETE(util.RemoveURI, i.handleCatalogDelete)
	r.GET("/:model/:version/:format", func(c *gin.Context) {
		var model ModelURI
		if err := c.ShouldBindUri(&model); err != nil {
			c.Status(http.StatusBadRequest)
			return
		}
		_, uriString := util.BuildImportKeyAndURI(model.Model, model.Version, i.format)
		i.lock.Lock()
		defer i.lock.Unlock()
		il, ok := i.content[uriString]
		if !ok {
			c.Status(http.StatusNotFound)
			return
		}
		klog.Infof("returning content: uriString %s with data of len %d", uriString, len(il.content))
		il.handleCatalogInfoGet(c)
	})
	r.GET(util.ModelCardURI, i.handleModelCardGet)
	return i
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

func (i *ImportLocationServer) loadFromStorage() (bool, error) {
	rc, msg, err, keys := i.storage.ListModelsKeys()
	if err != nil {
		klog.Errorf("%s: %s", err.Error(), msg)
		return false, nil
	}
	if rc != http.StatusOK {
		klog.Errorf("bad response code from storage list models %d, %s", rc, msg)
		return false, nil
	}

	for _, key := range keys {
		segs := strings.Split(key, "_")
		if len(segs) < 2 {
			klog.Errorf("bad format for key from ListModelsKeys when splitting with '_': %s", key)
			continue
		}
		il := &ImportLocation{}
		rc, msg, err, il.content = i.storage.FetchModel(key)
		if err != nil {
			klog.Errorf("%s: %s", err.Error(), msg)
			return false, nil
		}
		if rc != http.StatusOK {
			klog.Errorf("bad response code from storage fetch model %s is %d, %s", key, rc, msg)
			return false, nil
		}
		_, uri := util.BuildImportKeyAndURI(segs[0], segs[1], i.format)
		i.lock.Lock()
		defer i.lock.Unlock()
		i.content[uri] = il
		i.router.GET(uri, il.handleCatalogInfoGet)
	}

	return true, nil
}

func (i *ImportLocationServer) Run(stopCh <-chan struct{}) {
	ch := make(chan int)
	go func() {
		for {
			select {
			case <-ch:
				return
			default:
				err := i.router.Run(fmt.Sprintf(":%s", i.port))
				if err != nil {
					klog.Errorf("ERROR: gin-gonic run error %s", err.Error())
				}
			}
		}
	}()
	<-stopCh
	close(ch)
}

type ImportLocation struct {
	content []byte
}

func (i *ImportLocation) handleCatalogInfoGet(c *gin.Context) {
	if i.content == nil {
		c.Status(http.StatusNotFound)
		return
	}
	c.Data(http.StatusOK, "Content-Type: application/json", i.content)
}

type DicoveryResponse struct {
	Uris []string `json:"uris"`
}

func (i *ImportLocationServer) handleCatalogDiscoveryGet(c *gin.Context) {
	d := &DicoveryResponse{}
	i.lock.Lock()
	defer i.lock.Unlock()
	for uri, il := range i.content {
		//TODO normalizer id should be part of the model lookup URI a la "kubeflow/mnist/v1" or "kserve/mnist/v1"

		// since we cannot delete handlers from gin, when we delete a location, rather than removing from the map,
		// we set the contents field to nil, so we check for that before deciding to in include the URI
		if il.content != nil {
			d.Uris = append(d.Uris, uri)
		}
	}
	content, err := json.Marshal(d)
	if err != nil {
		c.Status(http.StatusInternalServerError)
		c.Error(err)
		return
	}
	c.Data(http.StatusOK, "Content-Type: application/json", content)
}

type ModelURI struct {
	Model   string `uri:"model" binding:"required"`
	Version string `uri:"version" binding:"required"`
	Format  string `uri:"format" binding:"required"`
}

func (u *ImportLocationServer) handleCatalogUpsertPost(c *gin.Context) {
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
		klog.Error(msg)
		c.Error(err)
		return
	}
	segs := strings.Split(key, "_")
	if len(segs) < 2 {
		c.Status(http.StatusBadRequest)
		c.Error(fmt.Errorf("bad key format: %s", key))
		return
	}
	//TODO normalizer id should be part of the model lookup URI
	_, uriString := util.BuildImportKeyAndURI(segs[0], segs[1], u.format)
	il := &ImportLocation{}
	il.content = postBody.Body
	u.lock.Lock()
	defer u.lock.Unlock()
	u.content[uriString] = il
	u.modelcards[postBody.ModelCardKey] = postBody.ModelCard
	klog.Infof("Upserting URI %s with data of len %d with modelcard key %s and modelcard len %d", uriString, len(postBody.Body), postBody.ModelCardKey, len(postBody.ModelCard))
	c.Status(http.StatusCreated)
}

func (u *ImportLocationServer) handleCatalogDelete(c *gin.Context) {
	key := c.Query("key")
	if len(key) == 0 {
		c.Status(http.StatusBadRequest)
		c.Error(fmt.Errorf("need a 'key' parameter"))
		return
	}
	segs := strings.Split(key, "_")
	if len(segs) < 2 {
		c.Status(http.StatusBadRequest)
		c.Error(fmt.Errorf("bad key format: %s", key))
		return
	}
	//TODO normalizer id should be part of the model lookup URI
	_, uri := util.BuildImportKeyAndURI(segs[0], segs[1], u.format)
	klog.Infof("Removing URI %s", uri)
	// you don't unbind URIs, so we remove its content regardless of removing it from the map so that
	// when backstage calls, we can return it a not found if the content is now nil
	u.lock.Lock()
	defer u.lock.Unlock()
	il, ok := u.content[uri]
	if ok {
		il.content = nil
	}
	c.Status(http.StatusOK)
}

func (i *ImportLocationServer) handleModelCardGet(c *gin.Context) {
     i.lock.Lock()
     defer i.lock.Unlock()
     key := c.Query(util.KeyQueryParam)
     content, ok := i.modelcards[key]
     if !ok {
          c.Status(http.StatusNotFound)
          return
     }
     c.Data(http.StatusOK, "Content-Type: text/markdown", []byte(content))

}