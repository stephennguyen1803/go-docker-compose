package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/callicoder/go-docker-compose/model"
	"github.com/go-redis/redis"
	"github.com/gorilla/mux"
	"go.mongodb.org/mongo-driver/bson"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type Order struct {
	OrderID string
	Total   float64
}

func respondWithError(w http.ResponseWriter, code int, message string) {
	respondWithJSON(w, code, map[string]string{"error": message})
}

func respondWithJSON(w http.ResponseWriter, code int, payload interface{}) {
	response, _ := json.Marshal(payload)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	w.Write(response)
}

func indexHandler(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte("Welcome! Please hit the `/qod` API to get the quote of the day."))
}

func connectMongo() (*mongo.Client, error) {
	clientOptions := options.Client().ApplyURI("mongodb://root:admin123@localhost:27017")
	client, err := mongo.Connect(context.TODO(), clientOptions)

	if err != nil {
		log.Fatal(err)
	}

	return client, err
}

func createOrder(w http.ResponseWriter, r *http.Request) {
	client, err := connectMongo()
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "can not connect Mongo")
	}

	w.Header().Set("Content-Type", "application/json")
	var order Order
	err = json.NewDecoder(r.Body).Decode(&order)

	if err != nil {
		respondWithError(w, http.StatusBadRequest, "can not json decode")
		return
	}

	quickstartDatabase := client.Database("MiddleWare")
	ordersCollection := quickstartDatabase.Collection("Orders")

	_, err = ordersCollection.InsertOne(context.TODO(), bson.D{
		{Key: "OrderId", Value: order.OrderID},
		{Key: "Total", Value: order.Total},
	})

	if err != nil {
		respondWithError(w, http.StatusNotImplemented, err.Error())
		return
	}

	var message = "Order: " + order.OrderID + "created success"
	respondWithJSON(w, http.StatusOK, message)
	return
}

func quoteOfTheDayHandler(client *redis.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		currentTime := time.Now()
		date := currentTime.Format("2006-01-02")

		val, err := client.Get(date).Result()
		if err == redis.Nil {
			log.Println("Cache miss for date ", date)
			quoteResp, err := getQuoteFromAPI()
			if err != nil {
				w.Write([]byte("Sorry! We could not get the Quote of the Day. Please try again."))
				return
			}
			quote := quoteResp.Contents.Quotes[0].Quote
			client.Set(date, quote, 24*time.Hour)
			w.Write([]byte(quote))
		} else {
			log.Println("Cache Hit for date ", date)
			w.Write([]byte(val))
		}
	}
}

func main() {
	// Create Redis Client
	client := redis.NewClient(&redis.Options{
		Addr:     getEnv("REDIS_URL", "localhost:6379"),
		Password: getEnv("REDIS_PASSWORD", ""),
		DB:       0,
	})

	_, err := client.Ping().Result()
	if err != nil {
		log.Fatal(err)
	}

	// Create Server and Route Handlers
	r := mux.NewRouter()

	r.HandleFunc("/", indexHandler)
	r.HandleFunc("/qod", quoteOfTheDayHandler(client))

	r.HandleFunc("/createOrder", createOrder).Methods("POST")

	srv := &http.Server{
		Handler:      r,
		Addr:         ":8080",
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	// Start Server
	go func() {
		log.Println("Starting Server")
		if err := srv.ListenAndServe(); err != nil {
			log.Fatal(err)
		}
	}()

	// Graceful Shutdown
	waitForShutdown(srv)
}

func waitForShutdown(srv *http.Server) {
	interruptChan := make(chan os.Signal, 1)
	signal.Notify(interruptChan, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	// Block until we receive our signal.
	<-interruptChan

	// Create a deadline to wait for.
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()
	srv.Shutdown(ctx)

	log.Println("Shutting down")
	os.Exit(0)
}

func getQuoteFromAPI() (*model.QuoteResponse, error) {
	API_URL := "http://quotes.rest/qod.json"
	resp, err := http.Get(API_URL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	log.Println("Quote API Returned: ", resp.StatusCode, http.StatusText(resp.StatusCode))

	if resp.StatusCode >= 200 && resp.StatusCode <= 299 {
		quoteResp := &model.QuoteResponse{}
		json.NewDecoder(resp.Body).Decode(quoteResp)
		return quoteResp, nil
	} else {
		return nil, errors.New("Could not get quote from API")
	}

}

func getEnv(key, defaultValue string) string {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	return value
}
