
export enum Status {
    STATUS_OK = 0,
    STATUS_CANCELLED = 1,
    STATUS_UNKNOWN = 2,
    STATUS_INVALID_ARGUMENT = 3,
    STATUS_DEADLINE_EXCEEDED = 4,
    STATUS_NOT_FOUND = 5,
    STATUS_ALREADY_EXISTS = 6,
    STATUS_PERMISSION_DENIED = 7,
    STATUS_RESOURCE_EXHAUSTED = 8,
    STATUS_FAILED_PRECONDITION = 9,
    STATUS_ABORTED = 10,
    STATUS_OUT_OF_RANGE = 11,
    STATUS_UNIMPLEMENTED = 12,
    STATUS_INTERNAL = 13,
    STATUS_UNAVAILABLE = 14,
    STATUS_DATA_LOSS = 15,
    STATUS_UNAUTHENTICATED = 16,
}

export interface getDirContentsRequest {
    Token: string;
    Path?: string;
}

export interface directoryListingItem {
    Name: string;
    IsDir?: boolean;
}
export interface getDirContentsResponse {
    Items?: directoryListingItem[];
    SomeOtherVal?: string;
}

export interface sayHelloRequest {
    input_name: string;
}

export interface sayHelloResponse {
    Message: string;
}

export async function getDirContents(params: getDirContentsRequest, headers?: HeadersInit): Promise<Response<getDirContentsResponse> | Error> {
	return genFunc<getDirContentsRequest, getDirContentsResponse>(params, "/tinyrpc/getDirContents", headers);
}

export async function sayHello(params: sayHelloRequest, headers?: HeadersInit): Promise<Response<sayHelloResponse> | Error> {
	return genFunc<sayHelloRequest, sayHelloResponse>(params, "/tinyrpc/sayHello", headers);
}

async function genFunc<T, K>(params: T, path: string, headers?: HeadersInit): Promise<Error | Response<K>> {
	const requestOptions: RequestInit = { method: "POST" };
	requestOptions.body = JSON.stringify(params as T);
	requestOptions.headers = headers;
	
	const host = "http://localhost:8000";
	const url = host + path;
	let res;
	try { res = await fetch(url, requestOptions); }
	catch (e) {
		return { Message: "Likely network error: " + e, Status: Status.STATUS_UNAVAILABLE, IsError: true } as Error;
	}
	let body;
	try { body = await res.json(); }
	catch (e) {
		// couldn't cast to JSON
		return { Message: e, Status: Status.STATUS_UNAVAILABLE, IsError: true } as Error;
	}
	// Check if it's an application error and try build into an Error response
	let innerBody = body["Body"];
	if (innerBody !== undefined && innerBody["ErrorMessage"] !== undefined) {
		try {
			let r = body as Response<ErrorRes>;
			return { Message: r.Body.ErrorMessage, Status: r.Status, IsError: true } as Error;
		} catch (e) {
			return { Message: e, Status: Status.STATUS_UNAVAILABLE, IsError: true } as Error
		}
	}
	try {
		let r = body as Response<K>;
		return r;
	} catch (e) {
		return { Message: e, Status: Status.STATUS_UNAVAILABLE, IsError: true } as Error
	}
}

export function isError(possibleError: Error | Response<any>): possibleError is Error {
	return (possibleError as Error).IsError !== undefined;
}

export interface Response<T> { Body: T; Status: Status; Headers: Headers; }
export interface Error { Message: String; IsError: boolean; Status: Status; }
export interface ErrorRes { ErrorMessage: String }
