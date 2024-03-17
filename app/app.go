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

	"github.com/concolorcarne/crosser/typescriptify"
	"github.com/go-playground/validator/v10"
	"github.com/gorilla/mux"
)

type Crosser struct {
	// Relax the type constraints here, as they'll be enforced when creating the handlers?
	handlers         []*RouteContainer
	host             string
	router           *mux.Router
	tsOutputLocation string
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

func (p *Route[input, output]) AttachWithMiddleware(app *Crosser, headerMiddleware []HeaderMiddlewareFn) {
	rr, err := p.createRouteRep(headerMiddleware)
	if err != nil {
		panic(err)
	}
	app.AddHandler(rr)
}

func (p *Route[input, output]) Attach(app *Crosser) {
	p.AttachWithMiddleware(app, []HeaderMiddlewareFn{})
}

// Take the RouteContainer and any header middleware, and return a standard HTTP handler
func buildHandler(query *RouteContainer, middleware []HeaderMiddlewareFn) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, req *http.Request) {
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
		if err != nil {
			jsonError, err := buildError(STATUS_INTERNAL, fmt.Sprintf("unable to execute handler: %v", err))
			if err != nil {
				http.Error(w, fmt.Sprintf("Unable to create json body: %v", err), 500)
				return
			}
			w.Write(jsonError)
			return
		}

		w.Write(res)
	}
}

func (c *Crosser) AddAdditionalHandlers(pathPrefix string, handler http.Handler) {
	c.router.PathPrefix("/static/").Handler(http.StripPrefix("/static/", http.FileServer(http.Dir("./static/"))))
	c.router.PathPrefix("/assets/").Handler(http.StripPrefix("/assets/", http.FileServer(http.Dir("./static/assets/"))))
}

// Take the handlers and register them on the router
func (c *Crosser) assembleHandlers() {
	for _, query := range c.handlers {
		f := buildHandler(query, query.HeaderMiddleware)
		fmt.Println("Attaching route at", query.QueryPath)
		c.router.HandleFunc(
			query.QueryPath,
			f,
		).Methods("POST")
	}
}

func (c *Crosser) writeCode() {
	if c.tsOutputLocation == "" {
		// Skip writing code out
		return
	}

	code, err := c.genCode()
	if err != nil {
		panic(err)
	}

	os.Remove(c.tsOutputLocation)
	err = os.WriteFile(c.tsOutputLocation, []byte(code), 0777)
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
	c.assembleHandlers()
	c.writeCode()

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

func (c *Crosser) genCode() (string, error) {
	converter := typescriptify.New()
	converter.DontExport = false
	converter.BackupDir = ""
	converter.CreateInterface = true
	converter.Quiet = true

	for _, qr := range c.handlers {
		converter.AddType(qr.InputType)
		converter.AddType(qr.OutputType)
		converter.AddFunction(typescriptify.TypeScriptFunction{
			IsAsync: true,
			Name:    qr.FnName,
			Parameters: []typescriptify.FunctionParameter{
				{Name: "params", Type: qr.InputType.Name()},
				{Name: "headers?", Type: "HeadersInit | undefined"},
			},
			ReturnType: fmt.Sprintf("Promise<Response<%s> | Error>", qr.OutputType.Name()),
			Body: []string{fmt.Sprintf(
				`return genFunc<%s, %s>(params, "%s", headers);`,
				qr.InputType.Name(),
				qr.OutputType.Name(),
				qr.QueryPath,
			)},
		})
	}

	// Generate the 'base' function, then generate the additional functions
	converter.AddFunction(typescriptify.TypeScriptFunction{
		IsAsync:    true,
		DontExport: true,
		Name:       "genFunc<T, K>",
		Parameters: []typescriptify.FunctionParameter{
			{Name: "params", Type: "T"},
			{Name: "path", Type: "string"},
			{Name: "headers?", Type: "HeadersInit | undefined"},
		},
		ReturnType: "Promise<Error | Response<K>>",
		Body: []string{
			`const requestOptions: RequestInit = { method: "POST" };`,
			`requestOptions.body = JSON.stringify(params as T);`,
			`requestOptions.headers = headers;`,
			``,
			fmt.Sprintf(`const host = "http://%s";`, c.host),
			`const url = host + path;`,
			// Generate the code to handle fetch function errors
			`let res;`,
			`try { res = await fetch(url, requestOptions); }`,
			`catch (e) {`,
			`	return { Message: "Likely network error: " + e, Status: Status.STATUS_UNAVAILABLE, IsError: true } as Error;`,
			`}`,

			// Generate the code to handle non-JSON response errors
			`let body;`,
			`try { body = await res.json(); }`,
			`catch (e) {`,
			`	// couldn't cast to JSON`,
			`	return { Message: e, Status: Status.STATUS_UNAVAILABLE, IsError: true } as Error;`,
			`}`,

			// Generate the code to handle the application returning an error
			`// Check if it's an application error and try build into an Error response`,
			`let innerBody = body["Body"];`,
			`if (innerBody !== undefined && innerBody["ErrorMessage"] !== undefined) {`,
			`	try {`,
			`		let r = body as Response<ErrorRes>;`,
			`		return { Message: r.Body.ErrorMessage, Status: r.Status, IsError: true } as Error;`,
			`	} catch (e) {`,
			`		return { Message: e, Status: Status.STATUS_UNAVAILABLE, IsError: true } as Error`,
			`	}`,
			`}`,

			`try {`,
			`	let r = body as Response<K>;`,
			`	return r;`,
			`} catch (e) {`,
			`	return { Message: e, Status: Status.STATUS_UNAVAILABLE, IsError: true } as Error`,
			`}`,
		},
	})

	converter.AddFunction(typescriptify.TypeScriptFunction{
		IsAsync:    false,
		DontExport: false,
		Name:       "isError",
		Parameters: []typescriptify.FunctionParameter{
			{Name: "possibleError", Type: "Error | Response<any>"},
		},
		ReturnType: "possibleError is Error",
		Body: []string{
			`return (possibleError as Error).IsError !== undefined;`,
		},
	})

	converter.AddEnum(AllStatus)

	code, err := converter.Convert(nil)
	if err != nil {
		return "", err
	}

	// Export the base response interface
	code += "\n"
	code += "export interface Response<T> { Body: T; Status: Status; Headers: Headers; }\n"
	code += "export interface Error { Message: String; IsError: boolean; Status: Status; }\n"
	code += "export interface ErrorRes { ErrorMessage: String }\n"

	return code, nil
}

// I can use handlers to build up a collection of types to generate
// Can I then also build the actual HTTP handlers
func (c *Crosser) AddHandler(q *RouteContainer) {
	c.handlers = append(c.handlers, q)
}
