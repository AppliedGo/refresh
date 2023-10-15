package main

import (
	"log"
	"sync"
	"testing"
	"time"
)

func TestAPIClient_token(t *testing.T) {
	log.Println("Starting test")
	a := NewAPIClient()

	client := func(n int, apiClient *APIClient, done <-chan struct{}, wg *sync.WaitGroup) {
		for {
			select {
			case <-done:
				wg.Done()
				log.Printf("Client %d done\n", n)
				return
			default:
				token, err := apiClient.token()
				log.Printf("Client %d token: %s, err: %v\n", n, token, err)
				time.Sleep(lifeSpan / 5)
			}
		}
	}

	var wg sync.WaitGroup
	done := make(chan struct{})
	log.Println("Starting clients")
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go client(i, a, done, &wg)
	}
	log.Println("Waiting for clients")
	<-time.After(5 * time.Second)
	log.Println("Stopping clients")
	close(done)
	log.Println("Waiting for clients to clean up")
	wg.Wait()

}
