package app

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/go-playground/validator/v10"
)

// Creates a new query procedure that can be attached to groups / app root.
// The generic arguments specify the structure for validating query parameters (the query Params and the resulting handler output)
func NewRoute[input any, output any](queryFn RouteHandler[input, output]) *Route[input, output] {
	if validate == nil {
		validate = validator.New(validator.WithRequiredStructEnabled())
	}

	var inputType input
	checkIfQueryStruct(inputType)

	var outputType input
	checkIfQueryStruct(outputType)

	return &Route[input, output]{
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

func extractRouteIOName[inputType any, outputType any]() (string, error) {
	// Cast the i/o struct names to a string, and pull out the relevant bit
	inputString := strings.Split(reflect.TypeFor[inputType]().String(), ".")[1]
	outputString := strings.Split(reflect.TypeFor[outputType]().String(), ".")[1]

	if inputString == outputString {
		return "", fmt.Errorf("the input and output parameters must have distinct structs")
	}

	inputName := strings.Replace(inputString, "Request", "", 1)
	outputName := strings.Replace(outputString, "Response", "", 1)
	if inputName != outputName {
		return "", fmt.Errorf("input and output structs should match the pattern {methodName}Request/{methodName}Response")
	}
	return inputName, nil
}

func (p *Route[input, output]) createRouteRep(headerMiddleware []HeaderMiddlewareFn) (*RouteContainer, error) {
	inputName, err := extractRouteIOName[input, output]()

	if err != nil {
		return nil, err
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
