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
		type getDirContentsRequest struct {
			Name string `validate:"nonzero"`
		}
		type getDirContentsResponse struct{ Out string }

		testFn := func(ctx context.Context, req getDirContentsRequest) (*getDirContentsResponse, error) {
			return &getDirContentsResponse{
				Out: req.Name,
			}, nil
		}

		newRoute := NewRoute(testFn)
		rr, err := newRoute.createRouteRep(nil)
		So(err, ShouldBeNil)
		handler := buildHandler(rr)

		Convey("with valid input", func() {
			input := getDirContentsRequest{
				Name: "testname",
			}
			inputJson, err := json.Marshal(input)
			So(err, ShouldBeNil)
			r, _ := http.NewRequest("POST", "/something", bytes.NewBuffer(inputJson))
			w := httptest.NewRecorder()

			handler(w, r)

			expectedRes := Res[getDirContentsResponse]{
				Status: STATUS_OK,
				Body: getDirContentsResponse{
					Out: "testname",
				},
			}

			expectedResJson, err := writeResponse(expectedRes)
			So(err, ShouldBeNil)
			body, _ := io.ReadAll(w.Body)
			So(string(body), ShouldEqualJSON, string(expectedResJson))
		})

		Convey("with invalid input", func() {
			inputJson := `{ "invalid_key": "invalid_value" }`
			r, _ := http.NewRequest("POST", "/something", bytes.NewBufferString(inputJson))
			w := httptest.NewRecorder()

			handler(w, r)

			So(err, ShouldBeNil)
			body, _ := io.ReadAll(w.Body)
			var bodyRes Res[ReturnError]
			_ = json.Unmarshal(body, &bodyRes)
			So(bodyRes.Status, ShouldEqual, STATUS_INVALID_ARGUMENT)
			So(bodyRes.Body.ErrorMessage, ShouldContainSubstring, "Name: zero value")
		})
	})
}
