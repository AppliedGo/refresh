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
title = "Continuous data refresh, or: how to keep API calls authorized"
description = "Automatically refresh data in the background with a goroutine and channels, no mutex required."
author = "Christoph Berger"
email = "chris@appliedgo.net"
date = "2017-00-00"
draft = "true"
categories = ["Concurrent Programming"]
tags = ["refresh", "goroutine", "channel"]
articletypes = ["Tutorial"]
+++

An access token should be initialized and refreshed from a central place, yet be available to upteenth of client sessions. Dynamic futures to the rescue.

<!--more-->

If web apps could sweat, they would.

On one end, hoardes of client sessions request continuous flow of data. On the other end, third-party APIs set rigorous access rules that require using short-lived access tokens.

Let an API access token expire and chaos starts. So better keep the tokens fresh!

But where many client sessions access a single API connection, you need to manage the token refresh centrally and distribute the current token to client sessions.

I am sure that there are many ways to do this, but here is one that is quick to implement and easy to understand. I call this approach "dynamic futures", in lack of a standard (or at least, a better) term.

## Dynamic futures

If the word "futures" lets you think of our planet rather than programming paradigms, hop over to [this article](https://appliedgo.net/futures). (TL;DR: A future is a variable whose value does not exist initially but will be available after it gets computed. In Go, a future can be trivially implemented as a goroutine with a result channel.)



## The code
*/

// ## Imports and globals
package main

import (
	"encoding/hex"
	"fmt"
	"log"
	"time"

	"golang.org/x/sync/singleflight"

	"crypto/rand"
	rnd "math/rand"
)

const (
	lifeSpan         = 100 * time.Millisecond
	failureRate      = 0.2
	apiErrorDuration = 2 * lifeSpan
)

type tokenResponse struct {
	Token string
	Err   error
}

type APIClient struct {
	tok chan tokenResponse
	g   *singleflight.Group
}

func NewAPIClient() *APIClient {
	a := &APIClient{
		tok: make(chan tokenResponse),
		g:   &singleflight.Group{},
	}
	go a.refreshToken() // This call sets a.token and a.apiErr.
	return a
}

var tempError bool

// `authorize()` simulates fetching a new access token that expires after `lifespan` milliseconds.
func authorize() (token string, lifespan time.Duration, err error) {
	b := make([]byte, 8)

	_, err = rand.Read(b)
	if err != nil {
		return "randError", lifeSpan, err
	}

	if rnd.Float64() < failureRate && !tempError {
		log.Println("API error")
		tempError = true
		go func() {
			<-time.After(apiErrorDuration)
			log.Println("API error resolved")
			tempError = false
		}()
	}

	if tempError {
		return "tempError", lifeSpan, fmt.Errorf("temporary API error")
	}

	return hex.EncodeToString(b), lifeSpan, err

}

// The `token()` method returns the current token or an error. Here is where the future is avalutated
func (a *APIClient) token() (string, error) {
	t := <-a.tok
	return t.Token, t.Err
}

// refreshToken starts a goroutine that fetches a new access token from the Amadeus authorization API if there is none yet, or if the current one expires. It returns channels for returning the current token, or an error if the token could not be fetched.
func (a *APIClient) refreshToken() {
	var token string
	var expiration time.Duration
	var err error
	// var once *sync.Once

	expired := time.After(0) // The timer fires immediately. This makes the `for` loop fetch a new token right away in the first iteration.

	for {
		select {
		// The expiration timer has fired and wrote the current time to `expired`.
		case <-expired:
			type res struct {
				t string
				e time.Duration
			}
			// Fetch a new token from the API.
			// The singleflight group `g` ensures that only one goroutine fetches the token at a time. All other goroutines that use the same key (a.token) will block and receive the shared result from g.Do()`.
			r, err, _ := a.g.Do(token, func() (any, error) {
				// This is a closure; we can set the outer variables directly.
				token, expiration, err = authorize()
				return res{token, expiration}, err
			})
			token = r.(res).t
			expiration = r.(res).e
			if err != nil {
				a.tok <- tokenResponse{Token: token, Err: err}

			}
			// Set a new timer to fire shortly before the token expires.
			expired = time.After(expiration * 9 / 10)

		case a.tok <- tokenResponse{Token: token}:
			// Someone has read the token, nothing to do.
			// The next iteration will send the token to the channel again.
		}
	}
}

func main() {
	fmt.Println(authorize())
	c := NewAPIClient()
	t, err := c.token()
	if err != nil {
		panic(err)
	}
	fmt.Println(t)
}

/*
## How to get and run the code

Step 1: `go get` the code. Note the `-d` flag that prevents auto-installing
the binary into `$GOPATH/bin`.

    go get -d github.com/appliedgo/TODO:

Step 2: `cd` to the source code directory.

    cd $GOPATH/src/github.com/appliedgo/TODO:

Step 3. Run the binary.

    go run TODO:.go


## Odds and ends
## Some remarks
## Tips
## Links


**Happy coding!**

*/
