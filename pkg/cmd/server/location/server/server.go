package server

import (
	"encoding/json"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/redhat-ai-dev/model-catalog-bridge/pkg/rest"
	"github.com/redhat-ai-dev/model-catalog-bridge/pkg/util"
	"k8s.io/klog/v2"
	"net/http"
	"strings"
)

type ImportLocationServer struct {
	router  *gin.Engine
	content map[string]*ImportLocation
}

func NewImportLocationServer(content map[string]*ImportLocation) *ImportLocationServer {
	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()
	i := &ImportLocationServer{
		router:  r,
		content: content,
	}
	klog.Infof("NewImportLocationServer content len %d", len(content))
	r.SetTrustedProxies(nil)
	r.TrustedPlatform = "X-Forwarded-For"
	r.Use(addRequestId())
	d := &DicoveryResponse{Uris: []string{}}
	for key, data := range content {
		klog.Infof("NewImportLocationServer looking at key %s and content len %d", key, len(data.content))
		il := &ImportLocation{content: data.content}
		segs := strings.Split(key, "_")
		if len(segs) < 2 {
			continue
		}
		_, uri := util.BuildImportKeyAndURI(segs[0], segs[1])
		klog.Infoln("Adding URI " + uri)
		r.GET(uri, il.handleCatalogInfoGet)
		d.Uris = append(d.Uris, uri)
	}
	r.GET(util.ListURI, i.handleCatalogDiscoveryGet)
	r.POST(util.UpsertURI, i.handleCatalogUpsertPost)
	r.DELETE(util.RemoveURI, i.handleCatalogDelete)
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

func (i *ImportLocationServer) Run(stopCh <-chan struct{}) {
	ch := make(chan int)
	go func() {
		for {
			select {
			case <-ch:
				return
			default:
				err := i.router.Run(":9090")
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
		klog.Errorf(msg)
		c.Error(fmt.Errorf(msg))
		return
	}
	segs := strings.Split(key, "_")
	if len(segs) < 2 {
		c.Status(http.StatusBadRequest)
		c.Error(fmt.Errorf("bad key format: %s", key))
		return
	}
	//TODO normalizer id should be part of the model lookup URI
	_, uri := util.BuildImportKeyAndURI(segs[0], segs[1])
	klog.Infof("Upserting URI %s with data of len %d", uri, len(postBody.Body))
	il, exists := u.content[uri]
	if !exists {
		il = &ImportLocation{}
		u.router.GET(uri, il.handleCatalogInfoGet)
	}
	il.content = postBody.Body
	u.content[uri] = il
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
	_, uri := util.BuildImportKeyAndURI(segs[0], segs[1])
	klog.Infof("Removing URI %s", uri)
	// there is no way to unregister a URI, so we remove its content regardless of removing it from the map so that
	// when backstage calls, we can return it a not found if the content is now nil
	il, ok := u.content[uri]
	if ok {
		il.content = nil
	}
	c.Status(http.StatusOK)
}
