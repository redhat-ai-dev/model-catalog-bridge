package server

import (
	"bytes"
	"github.com/gin-gonic/gin"
	"github.com/redhat-ai-dev/model-catalog-bridge/pkg/rest"
	"github.com/redhat-ai-dev/model-catalog-bridge/test/stub/common"
	testgin "github.com/redhat-ai-dev/model-catalog-bridge/test/stub/gin-gonic"
	"io"
	"k8s.io/apimachinery/pkg/util/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestHandleCatalogDiscoveryGet(t *testing.T) {
	for _, tc := range []struct {
		name              string
		content           map[string]*ImportLocation
		expectedSC        int
		expectedBody      string
		expectedBodyParts []string
	}{
		{
			name:         "no contents",
			expectedSC:   http.StatusOK,
			expectedBody: `{"uris":null}`,
		},
		{
			name:       "single line without content",
			expectedSC: http.StatusOK,
			content: map[string]*ImportLocation{
				"/mnist/v1/catalog": {},
			},
			expectedBody: `{"uris":null}`,
		},
		{
			name:       "single line with content",
			expectedSC: http.StatusOK,
			content: map[string]*ImportLocation{
				"/mnist/v1/catalog": {content: []byte{}},
			},
			expectedBody: `{"uris":["/mnist/v1/catalog"]}`,
		},
		{
			name:       "multi line",
			expectedSC: http.StatusOK,
			content: map[string]*ImportLocation{
				"/mnist/v1/catalog": {content: []byte{}},
				"/mnist/v2/catalog": {content: []byte{}},
			},
			// ordering not guaranteed on which uri is first
			expectedBodyParts: []string{"/mnist/v1/catalog", "/mnist/v2/catalog"},
		},
	} {
		testWriter := testgin.NewTestResponseWriter()
		ctx, _ := gin.CreateTestContext(testWriter)
		ils := &ImportLocationServer{content: tc.content}

		ils.handleCatalogDiscoveryGet(ctx)

		common.AssertEqual(t, ctx.Writer.Status(), tc.expectedSC)
		bodyBuf := testWriter.ResponseWriter.Body
		common.AssertNotNil(t, bodyBuf)
		if len(tc.expectedBody) > 0 {
			common.AssertEqual(t, tc.expectedBody, bodyBuf.String())
		}
		common.AssertContains(t, bodyBuf.String(), tc.expectedBodyParts)
	}
}

func TestHandleCatalogUpsertPost(t *testing.T) {
	// define outside of the test loop so we can vet updates vs. creates
	ils := &ImportLocationServer{content: map[string]*ImportLocation{}}
	for _, tc := range []struct {
		name            string
		reqURL          url.URL
		body            rest.PostBody
		expectedErrMsg  string
		expectedSC      int
		expectedContent map[string]*ImportLocation
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
			expectedContent: map[string]*ImportLocation{
				"/mnist/v1/catalog-info.yaml": {content: []byte("create")},
			},
		},
		{
			name:       "updated entry",
			reqURL:     url.URL{RawQuery: "key=mnist_v1"},
			body:       rest.PostBody{Body: []byte("update")},
			expectedSC: http.StatusCreated,
			expectedContent: map[string]*ImportLocation{
				"/mnist/v1/catalog-info.yaml": {content: []byte("update")},
			},
		},
	} {
		testWriter := testgin.NewTestResponseWriter()
		data, err := json.Marshal(tc.body)
		common.AssertError(t, err)
		ctx, eng := gin.CreateTestContext(testWriter)
		ctx.Request = &http.Request{URL: &tc.reqURL, Body: io.NopCloser(bytes.NewReader(data))}
		ils.router = eng

		ils.handleCatalogUpsertPost(ctx)

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

		common.AssertEqual(t, len(ils.content), len(tc.expectedContent))
		for key, val := range tc.expectedContent {
			v, ok := ils.content[key]
			common.AssertEqual(t, ok, true)
			common.AssertEqual(t, v, val)
		}
	}
}

func TestHandleCatalogDelete(t *testing.T) {
	for _, tc := range []struct {
		name            string
		reqURL          url.URL
		existingContent map[string]*ImportLocation
		expectedErrMsg  string
		expectedSC      int
		expectedContent map[string]*ImportLocation
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
			name:   "entry does not exist",
			reqURL: url.URL{RawQuery: "key=mnist_v2"},
			existingContent: map[string]*ImportLocation{
				"/mnist/v1/catalog-info.yaml": {content: []byte("create")},
			},
			expectedSC: http.StatusOK,
			expectedContent: map[string]*ImportLocation{
				"/mnist/v1/catalog-info.yaml": {content: []byte("create")},
			},
		},
		{
			name:   "entry exists",
			reqURL: url.URL{RawQuery: "key=mnist_v2"},
			existingContent: map[string]*ImportLocation{
				"/mnist/v1/catalog-info.yaml": {content: []byte("create")},
				"/mnist/v2/catalog-info.yaml": {content: []byte("create")},
			},
			expectedSC: http.StatusOK,
			expectedContent: map[string]*ImportLocation{
				"/mnist/v1/catalog-info.yaml": {content: []byte("create")},
				"/mnist/v2/catalog-info.yaml": {content: nil},
			},
		},
	} {
		testWriter := testgin.NewTestResponseWriter()

		ctx, eng := gin.CreateTestContext(testWriter)
		ctx.Request = &http.Request{URL: &tc.reqURL}
		ils := &ImportLocationServer{content: tc.existingContent}
		ils.router = eng

		ils.handleCatalogDelete(ctx)

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

		common.AssertEqual(t, len(ils.content), len(tc.expectedContent))
		for key, val := range tc.expectedContent {
			v, ok := ils.content[key]
			common.AssertEqual(t, ok, true)
			common.AssertEqual(t, v, val)
		}
	}
}
