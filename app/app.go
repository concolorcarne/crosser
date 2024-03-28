package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"reflect"
	"time"

	"github.com/gorilla/mux"
	"gopkg.in/validator.v2"
)

type TinyRPC struct {
	handlers         []*RouteContainer
	host             string
	router           *mux.Router
	tsOutputLocation string
	headerType       reflect.Type
	appConstants     any
}

func New(host string, tsOutputLocation string) *TinyRPC {
	return &TinyRPC{
		host:             host,
		router:           mux.NewRouter(),
		tsOutputLocation: tsOutputLocation,
	}
}

type ReturnError struct {
	ErrorMessage string
}

type Res[T any] struct {
	Body   T
	Status Status
}

type RouteHandler[input any, output any] func(ctx context.Context, query input) (*output, error)

type RouteContainer struct {
	InputType          reflect.Type
	OutputType         reflect.Type
	FnName             string
	HandleFn           func(context.Context, any) (any, error)
	QueryPath          string
	ChainedInterceptor []MiddlewareHandler
}

type Route[input any, output any] struct {
	// A handler that matches the shape of the generic function
	// but deals in bytes that are unmarshalled/ marshalled from/ to json
	byteHandler func(context.Context, any) (any, error)
}

func buildError(status Status, message string) ([]byte, error) {
	res := Res[ReturnError]{
		Status: status,
		Body: ReturnError{
			ErrorMessage: message,
		},
	}
	return writeResponse(res)
}

func writeResponse[T any](res Res[T]) ([]byte, error) {
	return json.Marshal(res)
}

// One of the slightly more mindbendy functions. Take a generic handler function and map to concrete (byte array) types, but
// embed the generic types in the unmarshal/ marshal
func queryToByteHandlerAdapter[inputType any, outputType any](queryFunc func(context.Context, inputType) (outputType, error)) func(context.Context, any) (any, error) {
	return func(ctx context.Context, input any) (any, error) {
		var body inputType
		err := json.Unmarshal(input.([]byte), &body)
		if err != nil {
			return buildError(STATUS_INVALID_ARGUMENT, err.Error())
		}

		err = validator.Validate(body)
		if err != nil {
			return buildError(STATUS_INVALID_ARGUMENT, err.Error())
		}

		res, err := queryFunc(ctx, body)
		if err != nil {
			return buildError(STATUS_INTERNAL, err.Error())
		}

		responseObject := Res[outputType]{
			Status: STATUS_OK,
			Body:   res,
		}
		return writeResponse(responseObject)
	}
}

func (p *Route[input, output]) AttachWithMiddleware(app *TinyRPC, headerMiddleware ...MiddlewareFn) {
	rr, err := p.createRouteRep(headerMiddleware)
	if err != nil {
		panic(err)
	}
	app.AddHandler(rr)
}

// Just an alias for AttachWithMiddleware
func (p *Route[input, output]) Attach(app *TinyRPC) {
	p.AttachWithMiddleware(app)
}

func padString(str string, maxLength int) string {
	// non-ascii characters take up more than one byte, whereas len(str) returns the number of bytes
	actualLength := len([]rune(str))
	return fmt.Sprintf("%s%*s", str, maxLength-actualLength, "")
}

type tinyRPCHeaderValue struct{}

var tinyRPCHeaderValueKey = tinyRPCHeaderValue{}

// I want to inject the HTTP Headers into the context here
func addHeadersToContext(ctx context.Context, headers http.Header) context.Context {
	headerMap := make(map[string]string)
	for key, value := range headers {
		headerMap[key] = value[0]
	}
	return context.WithValue(ctx, tinyRPCHeaderValueKey, headerMap)
}

func GetHeader(ctx context.Context, key string) string {
	headers := ctx.Value(tinyRPCHeaderValueKey)
	if headers == nil {
		return ""
	}
	canonicalKey := http.CanonicalHeaderKey(key)
	return headers.(map[string]string)[canonicalKey]
}

// Take the RouteContainer and any header middleware, and return a standard HTTP handler
func buildHandler(query *RouteContainer) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, req *http.Request) {
		ctx := addHeadersToContext(req.Context(), req.Header)

		// Get request in the form of whatever, attempt to parse into expected structure
		body, err := io.ReadAll(req.Body)
		if err != nil {
			jsonError, err := buildError(STATUS_INTERNAL, fmt.Sprintf("unable to read from body: %v", err))
			if err != nil {
				http.Error(w, fmt.Sprintf("Unable to create json body: %v", err), 500)
				return
			}
			w.Write(jsonError)
			return
		}

		res, err := query.HandleFn(ctx, body)
		if err != nil {
			jsonError, err := buildError(STATUS_INTERNAL, fmt.Sprintf("unable to execute handler: %v", err))
			if err != nil {
				http.Error(w, fmt.Sprintf("Unable to create json body: %v", err), 500)
				return
			}
			w.Write(jsonError)
			return
		}

		w.Write(res.([]byte))
	}
}

// This is not good. It makes assumptions about the library user's file structure. Clean up
func (c *TinyRPC) AddAdditionalHandler(path string, handler http.Handler) {
	c.router.Handle(path, handler)
	// c.router.PathPrefix("/static/").Handler(http.StripPrefix("/static/", http.FileServer(http.Dir("./manage/static/"))))
	// c.router.PathPrefix("/assets/").Handler(http.StripPrefix("/assets/", http.FileServer(http.Dir("./manage/static/assets/"))))
}

// Take the handlers and register them on the router
func (c *TinyRPC) assembleHandlers() {
	longestIndex := 0
	for idx, query := range c.handlers {
		f := buildHandler(query)
		// We know that input and output types have to follow a particular pattern
		// so we can assume if something is the longest route, it's also longest
		// input and output
		if len(query.QueryPath) > len(c.handlers[longestIndex].QueryPath) {
			longestIndex = idx
		}

		c.router.HandleFunc(
			query.QueryPath,
			f,
		).Methods("POST")
	}

	// This'll be way more useful if I have the actual TS types at this point
	for _, query := range c.handlers {
		outputStr := padString(query.QueryPath, len(c.handlers[longestIndex].QueryPath))
		paddedInputString := padString(query.InputType.Name(), len(c.handlers[longestIndex].InputType.Name()))
		paddedOutputString := padString(query.OutputType.Name(), len(c.handlers[longestIndex].OutputType.Name()))

		outputStr += fmt.Sprintf("\t [%s -> %s]", paddedInputString, paddedOutputString)
		fmt.Println("Attaching: " + outputStr)
	}
}

func (c *TinyRPC) writeCode() {
	if c.tsOutputLocation == "" {
		// Skip writing code out
		fmt.Println("Not writing out code as tsOutputLocation is blank")
		return
	}

	code, err := c.genCode()
	if err != nil {
		panic(err)
	}

	// Call create on the existing file, which'll overwrite whatever was there
	f, err := os.Create(c.tsOutputLocation)
	if err != nil {
		panic(err)
	}

	_, err = f.Write([]byte(code))
	if err != nil {
		panic(err)
	}
}

func (c *TinyRPC) SeeAllRoutes() {
	c.router.Walk(func(route *mux.Route, router *mux.Router, ancestors []*mux.Route) error {
		tpl, err1 := route.GetPathTemplate()
		met, err2 := route.GetMethods()
		fmt.Println("======================", tpl, err1, met, err2)
		return nil
	})
}

func notFoundHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Println("Got not found request", r.URL)
	body, err := buildError(STATUS_NOT_FOUND, "Not found")
	if err != nil {
		w.WriteHeader(500)
		w.Write([]byte("Page not found"))
		return
	}
	w.WriteHeader(404)
	w.Write(body)
}

func (c *TinyRPC) Start() {
	start := time.Now()
	c.assembleHandlers()
	fmt.Printf("\nAssembled handlers in %v\n", time.Since(start))
	c.writeCode()
	fmt.Printf("%s %v\n\n", padString("Wrote code in", 21), time.Since(start))

	c.router.NotFoundHandler = http.HandlerFunc(notFoundHandler)

	// todo: Handle SSL
	addr := c.host
	srv := &http.Server{
		Handler:      c.router,
		Addr:         addr,
		WriteTimeout: 15 * time.Second,
		ReadTimeout:  15 * time.Second,
	}

	c.SeeAllRoutes()

	fmt.Println("Listening on:", addr)
	log.Fatal(srv.ListenAndServe())
}

func (c *TinyRPC) AddHeaderType(header any) {
	if c.headerType != nil {
		panic("Header type already set")
	}
	checkIfQueryStruct(header)

	c.headerType = reflect.TypeOf(header)
}

// I can use handlers to build up a collection of types to generate
// Can I then also build the actual HTTP handlers
func (c *TinyRPC) AddHandler(q *RouteContainer) {
	// Check that there's not already another handler on the same route
	for _, handler := range c.handlers {
		if handler.QueryPath == q.QueryPath {
			panic(fmt.Sprintf("Duplicate handler for route: %s", q.FnName))
		}
	}

	c.handlers = append(c.handlers, q)
}

func (c *TinyRPC) GetAllMethodNames() []string {
	returnSlice := make([]string, len(c.handlers))
	for idx, handler := range c.handlers {
		returnSlice[idx] = handler.FnName
	}
	return returnSlice
}

func (c *TinyRPC) AddAppConstants(appConstants any) {
	c.appConstants = appConstants
}
