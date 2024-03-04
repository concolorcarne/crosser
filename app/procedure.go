package app

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
)

type Method string
type validatorFn func(interface{}) error

const (
	TextXML               = "text/xml"
	TextHTML              = "text/html"
	TextPlain             = "text/plain"
	ApplicationXML        = "application/xml"
	ApplicationJSON       = "application/json"
	ApplicationJavaScript = "application/javascript"
	ApplicationForm       = "application/x-www-form-urlencoded"
	OctetStream           = "application/octet-stream"
	MultipartForm         = "multipart/form-data"

	TextXMLCharsetUTF8               = "text/xml; charset=utf-8"
	TextHTMLCharsetUTF8              = "text/html; charset=utf-8"
	TextPlainCharsetUTF8             = "text/plain; charset=utf-8"
	ApplicationXMLCharsetUTF8        = "application/xml; charset=utf-8"
	ApplicationJSONCharsetUTF8       = "application/json; charset=utf-8"
	ApplicationJavaScriptCharsetUTF8 = "application/javascript; charset=utf-8"
)

type Procedure[input any, output any] struct {
	jankedHandler func(context.Context, []byte) ([]byte, error)
}

func buildError(status int, message string) *Res[ReturnError] {
	return &Res[ReturnError]{
		Status: status,
		Body: ReturnError{
			Message: message,
		},
	}
}

func queryToJankedHandlerAdapter[queryType any, output any](queryFunc func(context.Context, queryType) (output, error)) func(context.Context, []byte) ([]byte, error) {
	return func(ctx context.Context, input []byte) ([]byte, error) {
		var body queryType
		if json.Unmarshal(input, &body) != nil {
			return nil, fmt.Errorf("invalid query type")
		}

		res, err := queryFunc(ctx, body)
		if err != nil {
			errorReturn := buildError(500, err.Error())
			return json.Marshal(errorReturn)
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
func NewQuery[input any, output any](queryFn Query[input, output]) *Procedure[input, output] {

	var queryInstance input
	checkIfQueryStruct(queryInstance)

	return &Procedure[input, output]{
		jankedHandler: queryToJankedHandlerAdapter(queryFn),
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

func (p *Procedure[input, output]) Attach(app *Crosser) {
	// I can check that the input and output match the required pattern
	inputString := strings.Split(reflect.TypeFor[input]().String(), ".")[1]
	outputString := strings.Split(reflect.TypeFor[output]().String(), ".")[1]

	if inputString == outputString {
		panic("They both need distinct types")
	}

	inputName := strings.Replace(inputString, "Request", "", 1)
	outputName := strings.Replace(outputString, "Response", "", 1)
	if inputName != outputName {
		panic("They should follow the pattern")
	}

	queryPath := fmt.Sprintf("/crosser/%s", inputName)

	app.AddHandler(&QueryRep{
		InputType:  reflect.TypeFor[input](),
		OutputType: reflect.TypeFor[output](),
		FnName:     inputName,
		HandleFn:   p.jankedHandler,
		QueryPath:  queryPath,
	})
}
