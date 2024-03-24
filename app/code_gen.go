package app

import (
	"encoding/json"
	"fmt"

	"github.com/concolorcarne/crosser/typescriptify"
)

func buildConvertHeaderFunction(headerParamSignature string) typescriptify.TypeScriptFunction {
	return typescriptify.TypeScriptFunction{
		IsAsync: false,
		Name:    "convertHeaders",
		Parameters: []typescriptify.FunctionParameter{
			{Name: "headers?", Type: headerParamSignature},
		},
		ReturnType: "Record<string, string>",
		Body: []string{
			`let r: Record<string, string> = {};`,
			`if (headers === undefined) { return r; }`,
			``,
			`Object.keys(headers).forEach((key) => {`,
			fmt.Sprintf(`	const v = headers[key as keyof %s]`, headerParamSignature),
			`	if( v !== undefined) { r[key] = v; }`,
			`})`,
			`return r`,
		},
	}
}

func buildGenFunc(headerParamSignature string, host string, shouldConvertHeaders bool) typescriptify.TypeScriptFunction {
	headerConversion := "headers"
	if shouldConvertHeaders {
		headerConversion = "convertHeaders(headers)"
	}

	return typescriptify.TypeScriptFunction{
		IsAsync:    true,
		DontExport: true,
		Name:       "genFunc<T, K>",
		Parameters: []typescriptify.FunctionParameter{
			{Name: "params", Type: "T"},
			{Name: "path", Type: "string"},
			{Name: "headers?", Type: headerParamSignature},
		},
		ReturnType: "Promise<Error | Response<K>>",
		Body: []string{
			`const requestOptions: RequestInit = { method: "POST" };`,
			`requestOptions.body = JSON.stringify(params as T);`,
			// Add the convertHeaders(headers) option if we're using a custom
			// header type
			fmt.Sprintf(`requestOptions.headers = %s;`, headerConversion),

			``,
			fmt.Sprintf(`const host = "http://%s";`, host),
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
	}
}

func (c *Crosser) genCode() (string, error) {
	converter := typescriptify.New()
	converter.DontExport = false
	converter.BackupDir = ""
	converter.CreateInterface = true
	converter.Quiet = true

	headerParamSignature := "HeadersInit"

	if c.headerType != nil {
		// We have a header type, lets add that type and reference it below
		converter.AddType(c.headerType)
		headerParamSignature = c.headerType.Name()
		converter.AddFunction(buildConvertHeaderFunction(headerParamSignature))
	}

	for _, qr := range c.handlers {
		converter.AddType(qr.InputType)
		converter.AddType(qr.OutputType)
		converter.AddFunction(typescriptify.TypeScriptFunction{
			IsAsync: true,
			Name:    qr.FnName,
			Parameters: []typescriptify.FunctionParameter{
				{Name: "params", Type: qr.InputType.Name()},
				{Name: "headers?", Type: headerParamSignature},
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
	converter.AddFunction(
		buildGenFunc(headerParamSignature, c.host, c.headerType != nil),
	)

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

	// Export the constants, if there are any. We don't need to export the type
	// as this is just an object of known shape
	if c.appConstants != nil {
		bytes, err := json.MarshalIndent(c.appConstants, "", "  ")
		if err != nil {
			return "", err
		}
		code += fmt.Sprintf("\nexport const AppConstants = %s;\n", string(bytes))
	}

	return code, nil
}
