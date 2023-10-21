/*
<!--
Copyright (c) 2019 Christoph Berger. Some rights reserved.

Use of the text in this file is governed by a Creative Commons Attribution Non-Commercial
Share-Alike License that can be found in the LICENSE.txt file.

Use of the code in this file is governed by a BSD 3-clause license that can be found
in the LICENSE.txt file.

The source code contained in this file may import third-party source code
whose licenses are provided in the respective license files.
-->

<!--
NOTE: The comments in this file are NOT godoc compliant. This is not an oversight.

Comments and code in this file are used for describing and explaining a particular topic to the reader. While this file is a syntactically valid Go source file, its main purpose is to get converted into a blog article. The comments were created for learning and not for code documentation.
-->

+++
title = "Continuous refresh, or: how to keep your API client authorized"
description = "Automatically refresh data in the background with a goroutine, channels, and a select statement. No mutex required."
author = "Christoph Berger"
email = "chris@appliedgo.net"
date = "2023-10-18"
draft = "true"
categories = ["Concurrent Programming"]
tags = ["refresh", "goroutine", "channel"]
articletypes = ["Tutorial"]
+++

An access token should be initialized and refreshed from a central place, yet be available to umpteenth of client sessions. Dynamic futures to the rescue.

<!--more-->

If web apps could sweat, they would.

On one end, hoards of client sessions request continuous flow of data. On the other end, third-party APIs set rigorous access rules that require using short-lived access tokens.

![Your app server, serving clients and accessing third-party APIs, needs to keep the access token fresh](refresh.svg)

Let an API access token expire and chaos starts. All open client sessions would run into errors! Bad user experience. So better keep the token fresh.

However, do not let each client session request a new access token for the same API. Only the last request wins, and all others have received a token that is already invalidated by subsequent refresh requests.

So if many client sessions access the same API, you need to manage the token refresh centrally and distribute the current token to client sessions.

I am sure that there are many ways to do this, but here is one that is quick to implement and easy to understand. I call this approach "dynamic futures", in lack of a standard (or at least, a better) term.

## Dynamic futures

If the word "futures" lets you think of our planet's potential destinies rather than programming paradigms, [this article](https://appliedgo.net/futures) gets you back on track.

TL;DR: A future is a variable whose value does not exist initially but will be available after it gets computed. In Go, a future can be trivially implemented as a goroutine with a result channel.

```go
result := make(chan int)

go func() {
	result <- compute()
}()

// other activity...

future := <-result
```

In this article, I want to leverage this mechanism to create a future that updates itself in the background. I pose several requirements to the implementation:

- All concurrent work shall be hidden from the client. No channels or other concurrency constructs shall leak to the client side. The client shall be able to always get a current and valid access token by a simple method call.
- Use standard Go concurrency ingredients only.
- Use the standard library only. (This rules out using [`singleflight`](https://pkg.go.dev/golang.org/x/sync/singleflight) or similar packages.)


## The challenge: orchestrating token refreshes and client requests

Scenario: A web app serves many clients. It also needs to call a third-party web API to fetch various data. This third-party API requires an access token that expires after a certain time. The web app must refresh the token on time, or else it would not be able to fetch more data from the API, and the client sessions would be blocked.

Furthermore, the web app must abide by the following rules:
1. The solution must not provide a client with an expired token
2. No client session should be blocked for an unacceptable amount of time

Let's see if we can implement all of these requirements and rules with minimal effort.

## The starting point: a future that can be read more than once

With a slight modification, we can make the "future" code from above return the computed result as often as we want.

```go
c := make(chan int)
n := 256

go func(input int, result chan<- int) {
	value := calculate(input)
	for {
		result <- value
	}
}(n, c)
```

The code can now read the channel as often as it wants.

```go
value1 := <-c
value2 := <-c  // Read again, get the same value again
```

## How to serve a value and compute a new one in the background

The above future implementation computes the result only once. To update the result in the background, we need a way of reading and writing the result in a concurrency-safe way.

Did anyone shout, "mutex!"?

Mutexes do work, but there is a better way: **a `select` statement.**

As a quick recap, a `select` statement is like a `switch` statement, except that the `case` conditions are channel read or write operations. That is, every `case` condition attempts to either read from or write to a channel. If a channel is not ready, the affected `case` blocks until the channel is ready. If more than one channel become ready for reading or writing, the `select` statement randomly selects one of the unblocked `case`s for execution. The other `case`s remain blocked until the running case completes.

This mechanism provides an elegant way of serializing access to resources and avoid data races, without having to use mutexes.

Here is how the `select` statement can help implement our "dynamic future":

```go
go func(token chan<- string) {
	tok, lifeSpan := authorize()
	expired := time.After(lifeSpan - safetyMargin)

	for {
		select {
			case token <- tok:
				// NOP
			case <-expired:
				tok, lifeSpan = authorize()
				expired = time.After(lifeSpan - safetyMargin)
		}
	}
}
```

### What happens here?

1. When the goroutine starts, it fetches an initial access token by calling function `authorize()` (that I will not define here, let's assume some black-box token refreshing logic). The function returns a new token and the token's lifespan (as a duration).
2. Then, the goroutine starts a timer that expires shortly before the token expires.
3. The goroutine enters an infinite loop that consists of a `select` statement with two cases.
4. The first case unblocks when a client attempts to read the `token` channel. The case sends the current token to the channel, and the loop starts over. Everything happens in the case condition, hence the case body is empty.
5. The second case unblocks when the timer fires. It fetches a new token and sets a new timer.

That's it! That's all the magic of our self-updating future.

### Why does this work?

The code makes use of the fundamental property of the `select` statement that I mentioned above.

The `select` statement randomly selects one of the unblocked `case`s for execution. It is not possible that two `case`s are unblocked at the same time. No data race can happen from simultaneous access to `tok` from both cases.

Hence, if a client reads the `token` channel, it can do so undisturbed.

And if the timer fires and the code calls the authorization API to refresh the access token, no client is able to read the current token anymore that can become invalidated at any time during the refresh.

But theory is cheap, so let's go through a working demo code.

## The code
*/

// ## Imports and globals
//
// The package name must be `main` to make the code run in the Go playground. In real life, the package would be a library package named, for example, `auth`.
package main

// None of these packages (except `time`) are actually required for the token refreshing code. They are used by the code that simulates the token refresh API, the test code, and for printing out what's going on.
import (
	"crypto/rand"
	"fmt"
	"log"
	rnd "math/rand"
	"sync"
	"testing"
	"time"
)

const (
	// We want to refresh the token *before* it expires. The `lifeSpanSafetyMargin` duration shall provide enough time for this.
	// For the simulation, it is set to an unrealistically small value, to make the test run fast.
	lifeSpanSafetyMargin = 10 * time.Millisecond
)

// The authorization API returns either a token or an error. We collect either of these in a `tokenResponse` and pass the result on to the client.
type tokenResponse struct {
	Token string
	Err   error
}

// `Token` represents an access token. It refreshes itself in the background by calling the API's authorization endpoint before the current token expires.
type Token struct {
	// The `accessToken` channel is used to send the new access token to the client.
	accessToken chan tokenResponse
	// The `authorize` field allows setting a custom authorization function that implements the call to the actual authorization endpoint.
	authorize func() (string, time.Duration, error)
}

// Method `refreshToken` fetches a new access token from the authorization API if there is none yet or if the current one expires. It sends the results (a token or an error) to the `accessToken` channel.
func (a *Token) refreshToken() {
	var token string
	var expiration time.Duration
	var err error

	// Set the initial token, before any client can request it.
	token, expiration, err = a.authorize()

	// Set a new timer to fire when 90% of the expiration duration has passed. We want a new token *before* the current one expires.
	expired := time.After(expiration - lifeSpanSafetyMargin)

	for {
		select {
		// When a client requests a token, this `case` condition writes one to the `accessToken` channel. It does nothing else, hence the body of the case is empty.
		case a.accessToken <- tokenResponse{Token: token, Err: err}:

		// The expiration timer has fired and wrote the current time to `expired`.
		case <-expired:
			// Refresh the token.
			log.Println("Token expired")
			token, expiration, err = a.authorize()
			if err != nil {
				log.Println("Error refreshing token:", err)
			} else {
				log.Println("Token refreshed")
			}
			// Set a new timer to fire when 90% of the expiration duration has passed.
			expired = time.After(expiration - lifeSpanSafetyMargin)

		}
	}
}

// The Token constructor receives the authorization function to call. It takes care of spawning the goroutine that refreshes the token in the background.
func NewToken(auth func() (string, time.Duration, error)) *Token {
	a := &Token{
		accessToken: make(chan tokenResponse),
		authorize:   auth,
	}
	go a.refreshToken() // This call sets a.token and a.apiErr.
	return a
}

// Method `Get()` returns the current token or an error.
func (a *Token) Get() (string, error) {
	t := <-a.accessToken
	return t.Token, t.Err
}

/*

### Simulating an authorization endpoint

Next, let me implement a flaky authorization function that we can pass to the `Token` constructor.

The function `authFunc()` simulates fetching a new access token that expires after `lifespan` milliseconds.

But the simulated authorization endpoint is not very stable. With a probability of `apiFailureRate`, the call to the endpoint fails, and the failure then persists for `apiErrorDuration`.

When this happens, the function simulates a half-heartedly backoff strategy ("sleep, then try just once again") that fails half of the time. (Exponential backoff with jitter, anyone? Take it as a homework assignment.)

Feel free to skip reading through that part of the code, it is not relevant for the implementation. For any production purposes, you would insert a real API call here.

*/

// These constants help simulate a not very reliable authorization endpoint.
// To finish the tests quickly, the durations are set to absurdly short values.
// (Typically, access tokens of web APIs have lifespans that are counted in
// minutes, not milliseconds.)

const (
	tokenLifeSpan       = 100 * time.Millisecond
	averageCallDuration = 8 * time.Millisecond
	apiFailureRate      = 0.2
	apiErrorDuration    = tokenLifeSpan * 150 / 100
)

// `tempError` is set to `true` during a simulated transient API/network outage.
var tempError bool

// `authFunc()` simulates fetching a new access token that expires after `lifespan` milliseconds.
func authFunc() (token string, lifespan time.Duration, err error) {
	b := make([]byte, 8)

	_, err = rand.Read(b)
	if err != nil {
		return "", tokenLifeSpan, err
	}

	// Simulate the delay of fetching a new token
	time.Sleep(averageCallDuration)

	// Simulate an API call error with a probability of `apiFailureRate`.
	// The error lasts for `apiErrorDuration`, then disappears.
	// The code pretends to do a backoff strategy that fails half of the time.
	if rnd.Float64() < apiFailureRate && !tempError {
		log.Println("API error")
		log.Println("Backing off...")
		time.Sleep(tokenLifeSpan)

		if rnd.Float64() < 0.5 {
			// Backoff strategy was not successful
			log.Println("API is still not back, giving up")
			tempError = true

			// The API/network outage resolves itself after `apiErrorDuration`.
			go func() {
				<-time.After(apiErrorDuration)
				log.Println("API error disappeared")
				tempError = false
			}()
		} else {
			log.Println("API error disappeared during backoff")
		}
	}

	if tempError {
		return "", tokenLifeSpan, fmt.Errorf("temporary API error")
	}

	return fmt.Sprintf("%x", b), tokenLifeSpan, nil

}

/*

## Testing

Instead of building a complete web app with an HTTP server, handlers, and more, let's just write a simple test that simulates a few clients requesting new tokens in regular intervals.

*/
//
func TestToken_get(t *testing.T) {
	log.SetFlags(0) // no extra log info
	log.Println("Starting test")
	a := NewToken(authFunc)

	// A test client requests an API token regularly, so that it can call the API.
	client := func(n int, token *Token, done <-chan struct{}, wg *sync.WaitGroup) {
		for {
			select {
			// The clients can be stopped by closing the done channel.
			case <-done:
				wg.Done()
				log.Printf("Client %d done\n", n)
				return
			// The clients shall request a token multiple times during the token's lifespan.
			default:
				t, err := token.Get()
				log.Printf("Client %d token: %s, err: %v\n", n, t, err)
				time.Sleep(tokenLifeSpan / 5)
			}
		}
	}

	// Start the clients in separate goroutines.
	var wg sync.WaitGroup
	done := make(chan struct{})
	log.Println("Starting clients")
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go client(i, a, done, &wg)
	}

	// After 5 seconds, stop the clients.
	<-time.After(5 * time.Second)
	log.Println("Stopping clients")
	close(done)
	log.Println("Waiting for clients to clean up")
	wg.Wait()

}

/*
## Run the code

The code runs fine in the Go playground and therefore is just [one click away](https://go.dev/play/p/wQYSNSrkbdi).

Alternatively, clone the [code repository](https://github.com/AppliedGo/refresh) and run `go test`.

```
git clone https://github.com/appliedgo/refresh
cd refresh
go test -v .
```


## Thoughts on using the code in real life

The code shown here is meant for demo purposes. To use it in production, consider a couple of improvements.

### Add a good backoff strategy

The above code's backoff "strategy" is to sleep once and try again.

A tested-and-proven backup strategy for production code is "exponential backoff with jitter". Exponential means that the time between retries becomes exponentially longer (for example, the first delay is 1 second, the second is 2 seconds the third is 4, the fourth is 8, and so on). Jitter means adding a random amount of time to the delay, to avoid that clients that happen to go into backoff at the same time all retry the call at the same times.

### Add contexts as needed

I omitted any `context` handling for brevity. Almost always, such code runs in a context that uses a `context` (pun not intended, the text formatting should give a hint on what I mean). You might want to cancel the token refresh loop (for example, on service shutdown) or add a timeout.

## Links

[Select statements (Go language specification)](https://go.dev/ref/spec#Select_statements)



**Happy coding!**

*/
