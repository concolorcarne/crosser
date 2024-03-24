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

	"github.com/go-playground/validator/v10"
	"github.com/gorilla/mux"
)

type Crosser struct {
	handlers         []*RouteContainer
	host             string
	router           *mux.Router
	tsOutputLocation string
	headerType       reflect.Type
	appConstants     any
}

func New(host string, tsOutputLocation string) *Crosser {
	return &Crosser{
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

type HeaderMiddlewareFn func(ctx context.Context, headers http.Header) error

type RouteContainer struct {
	InputType        reflect.Type
	OutputType       reflect.Type
	FnName           string
	HandleFn         func(context.Context, []byte) ([]byte, error)
	QueryPath        string
	HeaderMiddleware []HeaderMiddlewareFn
}

var validate *validator.Validate

type Route[input any, output any] struct {
	// A handler that matches the shape of the generic function
	// but deals in bytes that are unmarshalled/ marshalled from/ to json
	byteHandler func(context.Context, []byte) ([]byte, error)
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
func queryToByteHandlerAdapter[inputType any, outputType any](queryFunc func(context.Context, inputType) (outputType, error)) func(context.Context, []byte) ([]byte, error) {
	return func(ctx context.Context, input []byte) ([]byte, error) {
		var body inputType
		err := json.Unmarshal(input, &body)
		if err != nil {
			return nil, fmt.Errorf("invalid input schema: %v", err)
		}

		err = validate.Struct(body)
		if err != nil {
			return nil, fmt.Errorf("validation of body failed: %v", err)
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

func (p *Route[input, output]) AttachWithMiddleware(app *Crosser, headerMiddleware ...HeaderMiddlewareFn) {
	rr, err := p.createRouteRep(headerMiddleware)
	if err != nil {
		panic(err)
	}
	app.AddHandler(rr)
}

// Just an alias for AttachWithMiddleware
func (p *Route[input, output]) Attach(app *Crosser) {
	p.AttachWithMiddleware(app)
}

type perfDetail struct {
	routeName        string
	middlewareTiming time.Duration
	handlerTiming    time.Duration
}

func padString(str string, maxLength int) string {
	// non-ascii characters take up more than one byte, whereas len(str) returns the number of bytes
	actualLength := len([]rune(str))
	return fmt.Sprintf("%s%*s", str, maxLength-actualLength, "")
}

// Take the RouteContainer and any header middleware, and return a standard HTTP handler
func buildHandler(query *RouteContainer, middleware []HeaderMiddlewareFn) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, req *http.Request) {
		perf := perfDetail{
			routeName: query.FnName,
		}
		start := time.Now()

		// Run through the middleware function and break out if any of them fail
		for _, mw := range middleware {
			err := mw(req.Context(), req.Header)
			if err != nil {
				jsonError, err := buildError(STATUS_INTERNAL, fmt.Sprintf("unable to execute middleware: %v", err))
				if err != nil {
					http.Error(w, fmt.Sprintf("Unable to create json body: %v", err), 500)
					return
				}
				w.Write(jsonError)
				return
			}
		}
		perf.middlewareTiming = time.Since(start)
		start = time.Now()

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

		res, err := query.HandleFn(req.Context(), body)
		perf.handlerTiming = time.Since(start)
		if err != nil {
			jsonError, err := buildError(STATUS_INTERNAL, fmt.Sprintf("unable to execute handler: %v", err))
			if err != nil {
				http.Error(w, fmt.Sprintf("Unable to create json body: %v", err), 500)
				return
			}
			w.Write(jsonError)
			return
		}

		fmt.Printf(
			"%s %s %s\n",
			padString(fmt.Sprintf("[%s]", perf.routeName), 20),
			padString(fmt.Sprintf("Middleware: %v", perf.middlewareTiming), 30),
			fmt.Sprintf("Handler: %v", perf.handlerTiming),
		)

		w.Write(res)
	}
}

// This is not good. It makes assumptions about the library user's file structure. Clean up
func (c *Crosser) AddAdditionalHandlers() {
	c.router.PathPrefix("/static/").Handler(http.StripPrefix("/static/", http.FileServer(http.Dir("./static/"))))
	c.router.PathPrefix("/assets/").Handler(http.StripPrefix("/assets/", http.FileServer(http.Dir("./static/assets/"))))
}

// Take the handlers and register them on the router
func (c *Crosser) assembleHandlers() {
	longestRoute := 0
	longestInput := 0
	longestOutput := 0
	for _, query := range c.handlers {
		f := buildHandler(query, query.HeaderMiddleware)
		// We know that input and output types have to follow a particular pattern
		// so we can assume if something is the longest route, it's also longest
		// input and output
		if len(query.QueryPath) > longestRoute {
			longestRoute = len(query.QueryPath)
			longestInput = len(query.InputType.Name())
			longestOutput = len(query.OutputType.Name())
		}

		c.router.HandleFunc(
			query.QueryPath,
			f,
		).Methods("POST")
	}

	// Add spaces so all are the same length, I'm sure there's a nicer way to do this
	// This'll also be way more useful if I have the actual TS types at this point
	for _, query := range c.handlers {
		outputStr := padString(query.QueryPath, longestRoute)
		paddedInputString := padString(query.InputType.Name(), longestInput)
		paddedOutputString := padString(query.OutputType.Name(), longestOutput)

		outputStr += fmt.Sprintf("\t [%s -> %s]", paddedInputString, paddedOutputString)
		fmt.Println("Attaching: " + outputStr)
	}
}

func (c *Crosser) writeCode() {
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

func notFoundHandler(w http.ResponseWriter, r *http.Request) {
	body, err := buildError(STATUS_NOT_FOUND, "Not found")
	if err != nil {
		w.WriteHeader(500)
		w.Write([]byte("Page not found"))
		return
	}
	w.WriteHeader(404)
	w.Write(body)
}

func (c *Crosser) Start() {
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

	fmt.Println("Listening on:", addr)
	log.Fatal(srv.ListenAndServe())
}

func (c *Crosser) AddHeaderType(header any) {
	if c.headerType != nil {
		panic("Header type already set")
	}
	checkIfQueryStruct(header)

	c.headerType = reflect.TypeOf(header)
}

// I can use handlers to build up a collection of types to generate
// Can I then also build the actual HTTP handlers
func (c *Crosser) AddHandler(q *RouteContainer) {
	// Check that there's not already another handler on the same route
	for _, handler := range c.handlers {
		if handler.QueryPath == q.QueryPath {
			panic(fmt.Sprintf("Duplicate handler for route: %s", q.FnName))
		}
	}

	c.handlers = append(c.handlers, q)
}

func (c *Crosser) AddAppConstants(appConstants any) {
	c.appConstants = appConstants
}
