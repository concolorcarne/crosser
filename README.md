# Crosser

## What is Crosser?
Crosser is a project heavily inspired by [tRPC](https://github.com/trpc/trpc), [BlueRPC](https://github.com/blue-rpc/bluerpc) and the similar functionality in [Wails](https://github.com/wailsapp/wails). It attempts to address the challenge of type safety between a Go web server, and a Typescript frontend.

## Aims
- A great DX for common tasks
- Abstraction of the HTTP transport layer as much as makes sense
- Easy to dig into both the library and the generated code

## Usage
Usage is reasonably straightforward. You generate a basic handler:

```go
type  sayHelloRequest  struct {
	Name  string
}

type  sayHelloResponse  struct {
	Message  string
}

func  sayHelloHandler(_  context.Context, req  sayHelloRequest) (*sayHelloResponse, error) {
	return  &sayHelloResponse{
		Message: fmt.Sprintf("Hello, %s!", req.Name),
	}, nil
}
```

Then attach the handler to the application and start the app:

```go
a  :=  app.New("localhost:8000", "./output.ts")
app.NewRoute(sayHelloHandler).Attach(a)

a.Start()
```

When you boot up the application you'll see something a `output.ts` file generated. Inside it, amongst other things will be:
```typescript
export interface sayHelloRequest {
	Name?: string;
}

export interface sayHelloResponse {
	Message?: string;
}

export  async  function  sayHello(params: sayHelloRequest, headers?: HeadersInit):  Promise<Response<sayHelloResponse> |  Error> {
	return genFunc<sayHelloRequest, sayHelloResponse>(params, "/crosser/sayHello", headers);
}
```

There's additional boilerplate (like the `genFunc` function) which enables the application to work, but this can now called from your application:

```typescript
import { sayHello } from  '../output'
...
sayHello({ Name:  "Batman" }).then(res  => {
	console.log(res.Body?.Message);
}
```

Note, this example doesn't deal with error handling for you. There's an `isError` function built into the generated output that handles this:

```typescript
import { sayHello, isError} from '../output'
...
sayHello({ Name:  "Batman" }).then(res  => {
	if (isError(res)) {
		console.log("got error:", res)
		return
	}
	console.log(res.Body.Message);
}
```

Now there's no need to check for the presence of `Body`, as the Typescript compiler can detect that we've got a concrete type from `res`.

### Additional generation options
If you want to mark a field as required in the Typescript output, or change the name of the exported field, this can be achieved through tags on the structs:
```go
type  sayHelloRequest  struct {
	Name string `validate:"required" json:"input_name"`
}

type  sayHelloResponse  struct {
	Message string `validate:"required"`
}
```
Which will lead to:
```typescript
export interface sayHelloRequest {
	input_name: string;
}

export interface sayHelloResponse {
	Message: string;
}
```

The ergonomics of this might change, as I've found that I'm generally marking fields as required. They may be required by default, and explicitly marked as optional in the future.

### Middleware
It's possible to add middleware to requests using `AttachWithMiddleware` instead of `Attach`. An example middleware would look something like:

```go
func GetTokenMiddleware(ctx context.Context, headers http.Header) error {
	token := headers.Get("token")
	if token != "secret-token" {
		return fmt.Errorf("invalid secret token")
	}
	return nil
}
```

If the middleware returns an error, the handler as a whole will return an error. The ergonomics of this currently feels a little too _leaky_. It's likely to change in the future.

Note, there's an [example](./example) directory that shows a basic `main.go` file and the generated output.



## FAQ
### Why not gRPC?
Setting up a gRPC build pipeline is the right choice for more mature projects, and something like [buf](http://buf.build) aims to make that part of the process less painful. But if the idea of screwing around with build pipelines for a little personal project isn't super appealing to you, this project (or some of the ones highlighted above) may be better fit.

### Should I use this in production?
On the one hand, probably not as it's not been tested in production anywhere. On the other, under the hood it's just a wrapper around the basic Go HTTP handlers and a `mux` router, which have been tested heavily in production. It's up to you!

### How stable is the API?
Not very! I'm exploring the ergonomics of the API at the moment and it's liable to change dramatically in breaking ways if something is a better fit. One of the things that I'm exploring right now is how to handle typed middleware. What I've come up with doesn't feel optimal, so that's likely to change.

### How does this work under the hood?
I've tried to make the library implementation as simple as possible, but there's some inherent complexity in building this, especially in Go.

#### Routes
The type signature for `NewRoute` is
```go
func  NewRoute[input  any, output  any](queryFn  RouteHandler[input, output]) *Route[input, output] {
```

Go's compiler is smart enough to simplify this in actual usage to `NewRoute(sayHelloHandler)` provided `sayHelloHandler` conforms to a specific shape.

`NewRoute` spits out a `Route` of type `Route[input, output]`. That `Route` will have a `byteHandler` that is just the handler defined above, but instead of having a signature of types `input, output`, it handles byte arrays (which is what the json marshalling/ unmarshalling works with)

You can think of the `byteHandler` as:
```go
newByteHandler(handlerFn func(inputType) outputType) -> (func([]byte)  ([]byte))
```
where the function that's returned has the concrete types 'baked in'. This is to work around Go's limitations when handling generic arguments in methods on a struct.

Under the hood, it can be thought of as:
```go
newByteHandler(handlerFn func(inputType) outputType) (func([]byte) ([]byte)) {
	return func(input []byte) []byte{
		var body inputType
		concreteInput := json.Unmarshal(input, &body)
		// We now have a concrete input type to work with
		// in the handlerFn
		output := handlerFn(concreteInput)

		// convert output to bytes to conform to byteHandler
		outputBytes := json.Marshal(output)

		return outputBytes
	}
}
```
