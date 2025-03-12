package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/gin-gonic/gin"
	bksgcli "github.com/redhat-ai-dev/model-catalog-bridge/pkg/cmd/cli/backstage"
	"github.com/redhat-ai-dev/model-catalog-bridge/pkg/cmd/server/storage/configmap"
	"github.com/redhat-ai-dev/model-catalog-bridge/pkg/rest"
	"github.com/redhat-ai-dev/model-catalog-bridge/pkg/types"
	"github.com/redhat-ai-dev/model-catalog-bridge/pkg/util"
	"github.com/redhat-ai-dev/model-catalog-bridge/test/stub/backstage"
	"github.com/redhat-ai-dev/model-catalog-bridge/test/stub/common"
	testgin "github.com/redhat-ai-dev/model-catalog-bridge/test/stub/gin-gonic"
	"github.com/redhat-ai-dev/model-catalog-bridge/test/stub/location"
	"io"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
)

func Test_handleCatalogUpsertPost_handleCatalogCurrentKeySetPost_ConfigMap(t *testing.T) {
	locationCallback := sync.Map{}
	brts := location.CreateBridgeLocationServerWithCallbackMap(&locationCallback, t)
	defer brts.Close()
	backstageCallback := sync.Map{}
	bks := backstage.CreateBackstageServerWithCallbackMap(&backstageCallback, t)
	defer bks.Close()

	cmCl := fake.NewClientset().CoreV1()
	cm := &corev1.ConfigMap{}
	cm.Name = util.StorageConfigMapName
	_, err := cmCl.ConfigMaps(metav1.NamespaceDefault).Create(context.Background(), cm, metav1.CreateOptions{})
	common.AssertError(t, err)

	// handleCatalogUpsertPost testing

	for _, tc := range []struct {
		name           string
		reqURL         url.URL
		body           rest.PostBody
		expectedErrMsg string
		expectedSC     int
	}{
		{
			name:           "no query param",
			expectedSC:     http.StatusBadRequest,
			expectedErrMsg: "need a 'key' parameter",
		},
		{
			name:           "bad query param",
			reqURL:         url.URL{RawQuery: "key=mnist"},
			expectedSC:     http.StatusBadRequest,
			expectedErrMsg: "bad key format",
		},
		{
			name:       "new entry",
			reqURL:     url.URL{RawQuery: "key=mnist_v1"},
			body:       rest.PostBody{Body: []byte("create")},
			expectedSC: http.StatusCreated,
		},
		{
			name:       "updated entry",
			reqURL:     url.URL{RawQuery: "key=mnist_v1"},
			body:       rest.PostBody{Body: []byte("update")},
			expectedSC: http.StatusOK,
		},
	} {
		testWriter := testgin.NewTestResponseWriter()
		var data []byte
		data, err = json.Marshal(tc.body)
		common.AssertError(t, err)
		ctx, eng := gin.CreateTestContext(testWriter)
		ctx.Request = &http.Request{URL: &tc.reqURL, Body: io.NopCloser(bytes.NewReader(data))}

		cms := configmap.NewConfigMapBridgeStorageForTest(metav1.NamespaceDefault, cmCl)

		s := &StorageRESTServer{
			router:          eng,
			st:              cms,
			mutex:           sync.Mutex{},
			pushedLocations: map[string]*types.StorageBody{},
			locations:       location.SetupBridgeLocationRESTClient(brts),
			bkstg:           bksgcli.SetupBackstageTestRESTClient(bks),
		}

		s.handleCatalogUpsertPost(ctx)

		common.AssertEqual(t, ctx.Writer.Status(), tc.expectedSC)
		if len(tc.expectedErrMsg) > 0 {
			errors := ctx.Errors
			found := false
			for _, e := range errors {
				if strings.Contains(e.Error(), tc.expectedErrMsg) {
					found = true
					break
				}
			}
			common.AssertEqual(t, true, found)
		}

		keys := tc.reqURL.Query()
		if keys.Has(util.KeyQueryParam) && ctx.Writer.Status() == http.StatusCreated {
			val := keys.Get(util.KeyQueryParam)
			// storage cache should not be populated yet
			_, ok := s.pushedLocations[val]
			common.AssertEqual(t, false, ok)
			cm, err = cmCl.ConfigMaps(metav1.NamespaceDefault).Get(context.Background(), util.StorageConfigMapName, metav1.GetOptions{})
			common.AssertError(t, err)
			_, ok = cm.BinaryData[val]
			common.AssertEqual(t, true, ok)

			found := false
			locationCallback.Range(func(key, value any) bool {
				found = true
				return true
			})
			// location service called
			common.AssertEqual(t, true, found)

			found = false
			keyList := []any{}
			backstageCallback.Range(func(key, value any) bool {
				found = true
				keyList = append(keyList, key)
				return true
			})
			// backstage called
			common.AssertEqual(t, true, found)

			// clear out call cache for next check
			for _, k := range keyList {
				backstageCallback.Delete(k)
			}
		}
		if keys.Has(util.KeyQueryParam) && ctx.Writer.Status() == http.StatusOK {
			val := keys.Get(util.KeyQueryParam)
			// storage cache should be populated
			_, ok := s.pushedLocations[val]
			common.AssertEqual(t, ok, true)

			// backstage should not be called again
			found := false
			backstageCallback.Range(func(key, value any) bool {
				found = true
				return true
			})
			common.AssertEqual(t, false, found)
		}

	}

	// handleCatalogCurrentKeySetPost testing

	// first, show that when the existing key set is present, the data remains in storage
	testWriter := testgin.NewTestResponseWriter()
	ctx, eng := gin.CreateTestContext(testWriter)
	ctx.Request = &http.Request{URL: &url.URL{RawQuery: "key=mnist_v1"}}
	cms := configmap.NewConfigMapBridgeStorageForTest(metav1.NamespaceDefault, cmCl)

	s := &StorageRESTServer{
		router:          eng,
		st:              cms,
		mutex:           sync.Mutex{},
		pushedLocations: map[string]*types.StorageBody{},
		locations:       location.SetupBridgeLocationRESTClient(brts),
		bkstg:           bksgcli.SetupBackstageTestRESTClient(bks),
	}

	s.handleCatalogCurrentKeySetPost(ctx)

	common.AssertEqual(t, http.StatusOK, ctx.Writer.Status())
	cm, err = cmCl.ConfigMaps(metav1.NamespaceDefault).Get(context.Background(), util.StorageConfigMapName, metav1.GetOptions{})
	common.AssertError(t, err)
	common.AssertEqual(t, 1, len(cm.BinaryData))

	// then no longer include keys, see model removed from storage

	// have to create a new Context as gin-gonic caches query params and does not expose a way to clear that cache
	ctx, _ = gin.CreateTestContext(testWriter)
	ctx.Request = &http.Request{URL: &url.URL{RawQuery: ""}}

	s.handleCatalogCurrentKeySetPost(ctx)

	common.AssertEqual(t, http.StatusOK, ctx.Writer.Status())
	cm, err = cmCl.ConfigMaps(metav1.NamespaceDefault).Get(context.Background(), util.StorageConfigMapName, metav1.GetOptions{})
	common.AssertError(t, err)
	common.AssertEqual(t, 0, len(cm.BinaryData))

	_, ok := locationCallback.Load("delete")
	common.AssertEqual(t, true, ok)

	_, ok = backstageCallback.Load("delete")
	common.AssertEqual(t, true, ok)

}
