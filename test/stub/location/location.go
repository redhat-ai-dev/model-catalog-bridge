package location

import (
	"encoding/json"
	"fmt"
	bridgeclient "github.com/redhat-ai-dev/model-catalog-bridge/pkg/cmd/server/location/client"
	"github.com/redhat-ai-dev/model-catalog-bridge/test/stub/backstage"
	"github.com/redhat-ai-dev/model-catalog-bridge/test/stub/common"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
)

func SetupBridgeLocationRESTClient(ts *httptest.Server) *bridgeclient.BridgeLocationRESTClient {
	bkstgTC := &bridgeclient.BridgeLocationRESTClient{}
	bkstgTC.RESTClient = common.DC()
	bkstgTC.UpsertURL = ts.URL
	bkstgTC.RemoveURL = ts.URL
	return bkstgTC
}

func CreateBridgeLocationServerWithCallbackMap(callback *sync.Map, t *testing.T) *httptest.Server {
	ts := common.CreateTestServer(func(w http.ResponseWriter, r *http.Request) {
		t.Logf("Method: %v", r.Method)
		t.Logf("Path: %v", r.URL.Path)

		switch r.Method {
		case common.MethodDelete:
			callback.Store("delete", "delete")
		case common.MethodPost:
			switch r.URL.Path {
			default:
				w.Header().Set("Content-Type", "application/json")
				bodyBuf, err := io.ReadAll(r.Body)
				if err != nil {
					_, _ = w.Write([]byte(fmt.Sprintf(common.TestPostJSONStringOneLinePlusBody, err.Error())))
					w.WriteHeader(500)
					return
				}
				if len(bodyBuf) == 0 {
					w.WriteHeader(500)
					return
				}
				data := backstage.Post{}
				err = json.Unmarshal(bodyBuf, &data)
				if err != nil {
					_, _ = w.Write([]byte(fmt.Sprintf(common.TestPostJSONStringOneLinePlusBody, err.Error())))
					w.WriteHeader(500)
					return
				}
				_, err = url.Parse(data.Target)
				if err != nil {
					w.WriteHeader(500)
					return
				}
				callback.Store("body", string(bodyBuf))
				_, _ = w.Write([]byte(fmt.Sprintf(common.TestPostJSONStringOneLinePlusBody, data.Target)))
				w.WriteHeader(201)

			}
		}
	})
	return ts
}

func CreateBridgeLocationServer(t *testing.T) *httptest.Server {
	return CreateBridgeLocationServerWithCallbackMap(&sync.Map{}, t)
}
