package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"golang.org/x/exp/rand"
)

var client *mongo.Client

var codeStore sync.Map

type ShortCode struct {
	Code    string `json:"code"`
	Expires int64  `json:"expires"`
}

type Customer struct {
	ID          primitive.ObjectID `json:"_id,omitempty" bson:"_id,omitempty"`
	FirstName   string             `json:"firstName,omitempty" bson:"firstName,omitempty"`
	LastName    string             `json:"lastName,omitempty" bson:"lastName,omitempty"`
	PhoneNumber string             `json:"phoneNumber,omitempty" bson:"phoneNumber,omitempty"`
	Score       float64            `json:"score" bson:"score"`
}

type VerificationRequest struct {
	Score float64 `json:"score"`
	Code  string  `json:"code"`
}

type HistoryRecord struct {
	CustomerID string  `bson:"customer_id"`
	Score      float64 `bson:"score"`
	Timestamp  int64   `bson:"timestamp"`
}

var dbName string = "pos"

func GetCustomersEndpoint(response http.ResponseWriter, request *http.Request) {
	response.Header().Set("Content-Type", "application/json")
	var customers []Customer
	collection := client.Database(dbName).Collection("customer")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cursor, err := collection.Find(ctx, bson.M{})
	if err != nil {
		log.Fatal(err)
	}
	defer cursor.Close(ctx)
	for cursor.Next(ctx) {
		var customer Customer
		cursor.Decode(&customer)
		customers = append(customers, customer)
	}
	json.NewEncoder(response).Encode(customers)
}

func CreateCustomerEndpoint(response http.ResponseWriter, request *http.Request) {
	response.Header().Set("Content-Type", "application/json")
	var customer Customer
	_ = json.NewDecoder(request.Body).Decode(&customer)
	collection := client.Database(dbName).Collection("customer")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, _ := collection.InsertOne(ctx, customer)
	json.NewEncoder(response).Encode(result)
}

func generateRandomCode() string {
	return strconv.Itoa(100000 + rand.Intn(900000))
}

func GenerateQRCodeEndpoint(response http.ResponseWriter, request *http.Request) {
	customerID := request.URL.Query().Get("customer_id")
	if customerID == "" {
		http.Error(response, "customer_id is required", http.StatusBadRequest)
		return
	}
	code := generateRandomCode()
	ttl := 5 * time.Minute
	expiration := time.Now().Add(ttl).Unix()
	codeStore.Store(code, customerID)
	json.NewEncoder(response).Encode(ShortCode{Code: code, Expires: expiration})
}

func VerifyCodeEndpoint(response http.ResponseWriter, request *http.Request) {
	var req VerificationRequest
	if err := json.NewDecoder(request.Body).Decode(&req); err != nil {
		http.Error(response, "Invalid request body", http.StatusBadRequest)
		return
	}

	fmt.Println("req.Code", req.Code)
	fmt.Println("req.Score", req.Score)

	if CustomerID, exists := codeStore.Load(req.Code); exists {
		fmt.Println("CustomerID", CustomerID)

		if CustomerID == nil || CustomerID.(string) == "" {
			http.Error(response, "Invalid customer ID", http.StatusBadRequest)
			return
		}

		// TODO open this
		// codeStore.Delete(req.Code)

		collection := client.Database(dbName).Collection("customer")
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		id, _ := primitive.ObjectIDFromHex(CustomerID.(string))
		fmt.Println("id", id)
		var customer Customer
		collection.FindOne(ctx, bson.M{"_id": id}).Decode(&customer)
		fmt.Println("customer", customer)
		newScore := customer.Score + req.Score
		fmt.Println("newScore", newScore)
		collection.UpdateOne(
			context.Background(),
			bson.M{"_id": id},
			bson.M{"$set": bson.M{"score": newScore}},
		)

		historyCollection := client.Database(dbName).Collection("history")
		historyRecord := HistoryRecord{
			CustomerID: CustomerID.(string),
			Score:      req.Score,
			Timestamp:  time.Now().Unix(),
		}
		historyCollection.InsertOne(context.Background(), historyRecord)

		response.WriteHeader(http.StatusOK)
		response.Write([]byte("Code verified and history saved"))
	} else {
		http.Error(response, "Invalid code", http.StatusBadRequest)
	}
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	clientOptions := options.Client().ApplyURI("mongodb://localhost:27017")
	client, _ = mongo.Connect(ctx, clientOptions)

	router := mux.NewRouter()
	router.HandleFunc("/api/customers", CreateCustomerEndpoint).Methods("POST")
	router.HandleFunc("/api/customers", GetCustomersEndpoint).Methods("GET")
	router.HandleFunc("/api/qrcode/generate", GenerateQRCodeEndpoint).Methods("GET")
	router.HandleFunc("/api/qrcode/verify", VerifyCodeEndpoint).Methods("POST")

	log.Fatal(http.ListenAndServe(":12345", router))
}
