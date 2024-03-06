package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestHandlerSomething(t *testing.T) {
	Convey("simple handler can be created", t, func() {
		type getDirContentsRequest struct{ Name string }
		type getDirContentsResponse struct{ Out string }

		testFn := func(ctx context.Context, req getDirContentsRequest) (*getDirContentsResponse, error) {
			return &getDirContentsResponse{
				Out: req.Name,
			}, nil
		}

		newRoute := NewRoute(testFn)
		rr, err := newRoute.createRouteRep(nil)
		So(err, ShouldBeNil)
		handler := buildHandler(rr, nil)

		input := getDirContentsRequest{
			Name: "testname",
		}
		inputJson, err := json.Marshal(input)
		So(err, ShouldBeNil)
		r, _ := http.NewRequest("POST", "/something", bytes.NewBuffer(inputJson))
		w := httptest.NewRecorder()

		handler(w, r)

		expectedRes := Res[getDirContentsResponse]{
			Status: 200,
			Body: getDirContentsResponse{
				Out: "testname",
			},
		}

		expectedResJson, err := json.Marshal(expectedRes)
		So(err, ShouldBeNil)
		body, _ := io.ReadAll(w.Body)
		So(string(body), ShouldEqualJSON, string(expectedResJson))
	})
}

// TestBuildHandler checks the HTTP handler's response for a given request.
func TestBuildHandler(t *testing.T) {
	Convey("basic test works", t, func() {
		// Mock RouteRep and middleware
		mockRouteRep := &RouteContainer{
			HandleFn: func(ctx context.Context, bytes []byte) ([]byte, error) {
				return json.Marshal(Res[returnError]{Status: 200, Body: returnError{Message: "Success"}})
			},
		}

		middleware := func(ctx context.Context, headers http.Header) error {
			return nil // Simulate successful middleware execution
		}

		handler := buildHandler(mockRouteRep, []HeaderMiddlewareFn{middleware})

		// Create a test server using our handler
		server := httptest.NewServer(http.HandlerFunc(handler))
		defer server.Close()

		// Prepare a request to send to the handler
		body := bytes.NewBufferString(`{}`)
		req, err := http.NewRequest("POST", server.URL, body)
		So(err, ShouldBeNil)

		// Execute the request
		resp, err := http.DefaultClient.Do(req)
		So(err, ShouldBeNil)
		defer resp.Body.Close()

		// Check the response status code
		So(resp.StatusCode, ShouldEqual, http.StatusOK)

		// Further checks can include inspecting the response body, headers, etc.
		// For example, decoding JSON response and verifying the expected output.
		var result Res[returnError]
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("Failed to decode response body: %v", err)
		}
	})

}
