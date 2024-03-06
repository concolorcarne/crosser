package app

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
)

type Procedure[input any, output any] struct {
	// A handler that matches the shape of the generic function
	// but deals in bytes that are unmarshalled/ marshalled from/ to json
	byteHandler func(context.Context, []byte) ([]byte, error)
}

func buildError(status int, message string) ([]byte, error) {
	res := Res[returnError]{
		Status: status,
		Body: returnError{
			Message: message,
		},
	}
	return json.Marshal(res)
}

func queryToByteHandlerAdapter[queryType any, output any](queryFunc func(context.Context, queryType) (output, error)) func(context.Context, []byte) ([]byte, error) {
	return func(ctx context.Context, input []byte) ([]byte, error) {
		var body queryType
		if json.Unmarshal(input, &body) != nil {
			return nil, fmt.Errorf("invalid query type")
		}

		res, err := queryFunc(ctx, body)
		if err != nil {
			return buildError(500, err.Error())
		}

		responseObject := Res[output]{
			Status: 200,
			Body:   res,
		}
		return json.Marshal(responseObject)
	}
}

// Creates a new query procedure that can be attached to groups / app root.
// The generic arguments specify the structure for validating query parameters (the query Params and the resulting handler output).
// Use any to avoid validation
func NewRoute[input any, output any](queryFn RouteHandler[input, output]) *Procedure[input, output] {

	var inputType input
	checkIfQueryStruct(inputType)

	var outputType input
	checkIfQueryStruct(outputType)

	return &Procedure[input, output]{
		byteHandler: queryToByteHandlerAdapter(queryFn),
	}
}

// This function should panic if the query params are not a struct or of type any (interface{})
func checkIfQueryStruct[query any](arg query) {
	queryT := reflect.TypeOf(arg) // Reflect on the zero value, not T directly

	//if this is an empty interface return
	if queryT == nil || (queryT.Kind() == reflect.Interface && queryT.NumMethod() == 0) {
		return
	}

	if queryT.Kind() == reflect.Ptr && queryT.Elem().Kind() != reflect.Struct {
		panic(fmt.Sprintf("generic argument Query must be a struct or a pointer to a struct or any (interface{}), got %s", queryT.Elem().Kind()))
	} else if queryT.Kind() != reflect.Struct {
		panic(fmt.Sprintf("generic argument Query must be a struct, got %s", queryT.Kind()))
	}
}

func (p *Procedure[input, output]) createRouteRep(headerMiddleware []HeaderMiddlewareFn) (*RouteContainer, error) {
	// I can check that the input and output match the required pattern
	inputString := strings.Split(reflect.TypeFor[input]().String(), ".")[1]
	outputString := strings.Split(reflect.TypeFor[output]().String(), ".")[1]

	if inputString == outputString {
		return nil, fmt.Errorf("the input and output parameters must have distinct structs")
	}

	inputName := strings.Replace(inputString, "Request", "", 1)
	outputName := strings.Replace(outputString, "Response", "", 1)
	if inputName != outputName {
		return nil, fmt.Errorf("input and output structs should match the pattern {methodName}Request/{methodName}Response")
	}

	queryPath := fmt.Sprintf("/crosser/%s", inputName)
	if headerMiddleware == nil {
		headerMiddleware = []HeaderMiddlewareFn{}
	}
	return &RouteContainer{
		InputType:        reflect.TypeFor[input](),
		OutputType:       reflect.TypeFor[output](),
		FnName:           inputName,
		HandleFn:         p.byteHandler,
		QueryPath:        queryPath,
		HeaderMiddleware: headerMiddleware,
	}, nil
}

func (p *Procedure[input, output]) Attach(app *Crosser, headerMiddleware []HeaderMiddlewareFn) {
	rr, err := p.createRouteRep(headerMiddleware)
	if err != nil {
		panic(err)
	}
	app.AddHandler(rr)
}
