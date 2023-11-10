package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"

	"github.com/gin-gonic/gin"
)

var broker *Broker
var LoopCompleted bool
var connectionsMutex sync.RWMutex

//var connectionsMutex sync.RWMutex

type Broker struct {

	// A map of clients, the keys of the map are the channels
	// over which we can push messages to attached clients.
	clients map[chan []byte]bool

	// Channel into which new clients can be pushed
	//
	newClients chan chan []byte

	// Channel into which disconnected clients should be pushed
	//
	defunctClients chan chan []byte

	// Channel into which messages are pushed to be broadcast out
	// to attahed clients.
	//
	messages chan []byte
}

func NewServer() (broker *Broker) {
	// Instantiate a broker

	broker = &Broker{
		make(map[chan []byte]bool),
		make(chan chan []byte),
		make(chan chan []byte),
		make(chan []byte),
	}

	return
}

func prepareChannel(rw http.ResponseWriter) {
	// Set the headers related to event streaming.
	rw.Header().Set("Content-Type", "text/event-stream")
	rw.Header().Set("Cache-Control", "no-cache")
	rw.Header().Set("Connection", "keep-alive")
	rw.Header().Set("Transfer-Encoding", "chunked")

	// Additional headers to see if it helps in improving the consistency of sse events
	// nginx configuration may need to be analyzed but for now we are trying via code changes.
	//https://serverfault.com/questions/801628/for-server-sent-events-sse-what-nginx-proxy-configuration-is-appropriate
	// if this does not help then the changes may be reverted
	rw.Header().Set("X-Accel-Buffering", "no")
	rw.Header().Set("proxy_buffering", "off")
	rw.Header().Set("proxy_cache", "off")
}

func sendDataUsingSSE(payload *map[string]any) {
	post, _ := json.Marshal(payload)
	broker.messages <- post
}

func (broker *Broker) handleClients(c *gin.Context) {
	rw := c.Writer
	flusher := rw.(http.Flusher)
	prepareChannel(rw)

	// Create a new channel,
	messageChan := make(chan []byte)

	// Add this client to the map of those that should
	// receive updates
	broker.newClients <- messageChan

	// Listen to the closing of the http connection via the CloseNotifier
	notify := rw.(http.CloseNotifier).CloseNotify()
	go func() {
		<-notify
		// Remove this client from the map of attached clients
		// when `EventHandler` exits.
		broker.defunctClients <- messageChan

	}()

	// Don't close the connection, instead loop endlessly.
	for {

		// Read from our messageChan.
		msg, open := <-messageChan

		if !open {
			// If our messageChan was closed, this means that the client has
			// disconnected.
			//fmt.Println("CLOSE the loop")
			break
		}

		fmt.Fprintf(rw, "data: %s\n\n", msg)

		flusher.Flush()
	}

}

func (b *Broker) Start() {

	// Start a goroutine
	//
	go func() {

		// Loop endlessly
		//
		for {

			// Block until we receive from one of the
			// three following channels.
			select {

			case s := <-b.newClients:
				connectionsMutex.RLock()
				b.clients[s] = true
				connectionsMutex.RUnlock()

			case s := <-b.defunctClients:
				connectionsMutex.Lock()

				//fmt.Println("Deleting client -> ",s)
				delete(b.clients, s)
				connectionsMutex.Unlock()
				close(s)

			case msg := <-b.messages:
				connectionsMutex.RLock()
				//	fmt.Println("Received message")
				for s := range b.clients {
					s <- msg
				}
				connectionsMutex.RUnlock()

			}
		}
	}()
}

func InitializeSSEServer() {
	broker = NewServer()
	// Set it running - listening and broadcasting events
	broker.Start()
}

func GetServerSentEventHandler(c *gin.Context) {

	tenant := c.Param("tenant")
	_, err := getTenantFromDB(tenant)
	if err != nil {
		c.JSON(http.StatusInternalServerError, "tenant not found")
		return
	}
	rw := c.Writer

	_, ok := rw.(http.Flusher)

	if !ok {
		c.JSON(http.StatusInternalServerError, "Streaming unsupported!")
		return
	}
	if broker == nil {
		InitializeSSEServer()
	}
	broker.handleClients(c)

}
