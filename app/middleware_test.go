package app

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

type middlewareContainer struct {
	RunCount         int
	CalledMethodName string
}

func (m *middlewareContainer) Middleware(ctx context.Context, req any, method string, handler MiddlewareHandler) (any, error) {
	m.RunCount++
	m.CalledMethodName = method
	return handler(ctx, req)
}

func TestMiddleware(t *testing.T) {
	Convey("simple handler with middleware works", t, func() {
		type getDirContentsRequest struct {
			Name string `validate:"nonzero"`
		}
		type getDirContentsResponse struct{ Out string }

		testFn := func(ctx context.Context, req getDirContentsRequest) (*getDirContentsResponse, error) {
			return &getDirContentsResponse{
				Out: req.Name,
			}, nil
		}

		Convey("with valid input", func() {
			newRoute := NewRoute(testFn)
			mwContainer := &middlewareContainer{}
			rr, err := newRoute.createRouteRep([]MiddlewareFn{mwContainer.Middleware})
			So(err, ShouldBeNil)
			handler := buildHandler(rr)

			input := getDirContentsRequest{
				Name: "testname",
			}
			inputJson, err := json.Marshal(input)
			So(err, ShouldBeNil)
			r, _ := http.NewRequest("POST", "/something", bytes.NewBuffer(inputJson))
			w := httptest.NewRecorder()

			handler(w, r)

			So(mwContainer.RunCount, ShouldEqual, 1)
			So(mwContainer.CalledMethodName, ShouldEqual, "getDirContents")
		})

		Convey("middleware chaining works", func() {
			newRoute := NewRoute(testFn)
			mwContainer := &middlewareContainer{}
			rr, err := newRoute.createRouteRep([]MiddlewareFn{mwContainer.Middleware, mwContainer.Middleware})
			So(err, ShouldBeNil)
			handler := buildHandler(rr)

			input := getDirContentsRequest{
				Name: "testname",
			}
			inputJson, err := json.Marshal(input)
			So(err, ShouldBeNil)
			r, _ := http.NewRequest("POST", "/something", bytes.NewBuffer(inputJson))
			w := httptest.NewRecorder()

			handler(w, r)

			So(mwContainer.RunCount, ShouldEqual, 2)
		})

		Convey("middleware should still be executed with invalid input", func() {
			newRoute := NewRoute(testFn)
			mwContainer := &middlewareContainer{}
			rr, err := newRoute.createRouteRep([]MiddlewareFn{mwContainer.Middleware})
			So(err, ShouldBeNil)
			handler := buildHandler(rr)

			inputJson := `{ "invalid_key": "invalid_value" }`
			r, _ := http.NewRequest("POST", "/something", bytes.NewBufferString(inputJson))
			w := httptest.NewRecorder()

			handler(w, r)

			So(mwContainer.RunCount, ShouldEqual, 1)
		})
	})
}
