package app

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"reflect"
	"time"

	"github.com/gorilla/mux"
	"github.com/tkrajina/typescriptify-golang-structs/typescriptify"
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

type returnError struct {
	Message string
}

type Res[T any] struct {
	Body   T
	Status int
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

func buildHandler(query *RouteContainer, middleware []HeaderMiddlewareFn) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, req *http.Request) {
		// Run through the middleware function and break out if any of them fail
		for _, mw := range middleware {
			err := mw(req.Context(), req.Header)
			if err != nil {
				jsonError, err := buildError(500, fmt.Sprintf("unable to execute middleware: %v", err))
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
			jsonError, err := buildError(500, fmt.Sprintf("unable to read from body: %v", err))
			if err != nil {
				http.Error(w, fmt.Sprintf("Unable to create json body: %v", err), 500)
				return
			}
			w.Write(jsonError)
			return
		}

		res, err := query.HandleFn(req.Context(), body)
		if err != nil {
			jsonError, err := buildError(500, fmt.Sprintf("unable to execute handler: %v", err))
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

func (c *Crosser) assembleHandlers() {
	for _, query := range c.handlers {
		// I know the input and output types. I need to map those to work
		// with func(http.ResponseWriter, *http.Request)
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

func (c *Crosser) Start() {
	c.assembleHandlers()
	c.writeCode()

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
			ReturnType: fmt.Sprintf("Promise<Response<%s>>", qr.OutputType.Name()),
			Body: fmt.Sprintf(
				`return genFunc<%s, %s>(params, "%s", headers);`,
				qr.InputType.Name(),
				qr.OutputType.Name(),
				qr.QueryPath,
			),
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
		ReturnType: "Promise<Response<K>>",
		Body: fmt.Sprintf(`
const requestOptions: RequestInit = { method: "POST" };

requestOptions.body = JSON.stringify(params as T);
requestOptions.headers = headers;
const host = "%s";

const url = host + path;
const res = await fetch(url, requestOptions);
let body = await res.json() as Response<K>;

return body;`, fmt.Sprintf("http://%s", c.host)),
	})

	code, err := converter.Convert(nil)
	if err != nil {
		return "", err
	}

	code += "\nexport interface Response<T> { Body: T; Status: number; Headers: Headers; }"

	return code, nil
}

// I can use handlers to build up a collection of types to generate
// Can I then also build the actual HTTP handlers
func (c *Crosser) AddHandler(q *RouteContainer) {
	c.handlers = append(c.handlers, q)
}
