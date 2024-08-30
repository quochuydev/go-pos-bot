package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/gorilla/mux"
	godotenv "github.com/joho/godotenv"
	"github.com/skip2/go-qrcode"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"golang.org/x/exp/rand"
	"gopkg.in/telebot.v3"
	tele "gopkg.in/telebot.v3"
)

var client *mongo.Client

var codeStore sync.Map

type ShortCode struct {
	Code    string `json:"code"`
	Expires int64  `json:"expires"`
}

type Customer struct {
	ID             primitive.ObjectID `json:"_id,omitempty" bson:"_id,omitempty"`
	FirstName      string             `json:"firstName,omitempty" bson:"firstName,omitempty"`
	Username       string             `json:"username,omitempty" bson:"username,omitempty"`
	TelegramUserId string             `json:"telegramUserId,omitempty" bson:"TelegramUserId,omitempty"`
	Score          float64            `json:"score" bson:"score"`
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

func VerifyCodeEndpoint(response http.ResponseWriter, request *http.Request) {
	var req VerificationRequest
	if err := json.NewDecoder(request.Body).Decode(&req); err != nil {
		http.Error(response, "Invalid request body", http.StatusBadRequest)
		return
	}

	fmt.Println("req.Code", req.Code)
	fmt.Println("req.Score", req.Score)

	if TelegramUserId, exists := codeStore.Load(req.Code); exists {
		fmt.Println("TelegramUserId", TelegramUserId)

		if TelegramUserId == nil || TelegramUserId.(string) == "" {
			http.Error(response, "Invalid customer ID", http.StatusBadRequest)
			return
		}

		// TODO open this
		// codeStore.Delete(req.Code)

		collection := client.Database(dbName).Collection("customer")
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		var customer Customer
		collection.FindOne(ctx, bson.M{"telegramUserId": TelegramUserId}).Decode(&customer)
		fmt.Println("customer", customer)
		newScore := customer.Score + req.Score
		fmt.Println("newScore", newScore)
		collection.UpdateOne(
			context.Background(),
			bson.M{"_id": customer.ID.String()},
			bson.M{"$set": bson.M{"score": newScore}},
		)

		historyCollection := client.Database(dbName).Collection("history")
		historyRecord := HistoryRecord{
			CustomerID: customer.ID.String(),
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

func NukeEndpoint(response http.ResponseWriter, request *http.Request) {
	client.Database(dbName).Drop(context.Background())
}

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Fatalf("Error loading .env file")
	}

	token := os.Getenv("TELEGRAM_TOKEN")
	if token == "" {
		log.Fatalf("TELEGRAM_TOKEN is not set in .env file")
	}

	mongoUrl := os.Getenv("MONGO_URL")
	if mongoUrl == "" {
		log.Fatalf("MONGO_URL is not set in .env file")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	clientOptions := options.Client().ApplyURI(os.Getenv("MONGO_URL"))
	client, _ = mongo.Connect(ctx, clientOptions)

	pref := tele.Settings{
		Token:  os.Getenv("TELEGRAM_TOKEN"),
		Poller: &tele.LongPoller{Timeout: 10 * time.Second},
	}

	b, err := tele.NewBot(pref)
	if err != nil {
		log.Fatal(err)
		return
	}

	b.Handle("/start", func(c tele.Context) error {
		user := c.Sender()
		telegramUserId := fmt.Sprint(user.ID)
		customer := Customer{
			FirstName:      user.FirstName,
			Username:       user.Username,
			TelegramUserId: telegramUserId,
			Score:          0,
		}

		collection := client.Database(dbName).Collection("customer")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		collection.InsertOne(ctx, customer)
		return c.Send("Hello " + user.FirstName)
	})

	b.Handle("/get_points_code", func(c tele.Context) error {
		user := c.Sender()
		code := generateRandomCode()
		codeStore.Store(code, fmt.Sprint(user.ID))
		c.Send("Mã tích điểm: " + code)

		qrCode, err := qrcode.New(code, qrcode.Medium)
		if err != nil {
			return c.Send("Failed to generate QR code")
		}

		var qrBuffer bytes.Buffer
		qrCode.Write(200, &qrBuffer)

		imageFile := telebot.File{
			FileReader: bytes.NewReader(qrBuffer.Bytes()),
		}

		photo := &telebot.Photo{
			File: imageFile,
		}

		return c.Send(photo)
	})

	b.Handle("/redeem_points", func(c tele.Context) error {
		user := c.Sender()
		collection := client.Database(dbName).Collection("customer")
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		var customer Customer
		collection.FindOne(ctx, bson.M{"TelegramUserId": fmt.Sprint(user.ID)}).Decode(&customer)
		score := fmt.Sprint(customer.Score)
		return c.Send("Hello " + customer.FirstName + " bạn có: " + score + " điểm")
	})

	go func() {
		fmt.Println("Starting Telegram bot...")
		b.Start()
	}()

	router := mux.NewRouter()
	router.HandleFunc("/api/nuke", NukeEndpoint).Methods("GET")
	router.HandleFunc("/api/customers", CreateCustomerEndpoint).Methods("POST")
	router.HandleFunc("/api/customers", GetCustomersEndpoint).Methods("GET")
	router.HandleFunc("/api/qrcode/verify", VerifyCodeEndpoint).Methods("POST")
	log.Fatal(http.ListenAndServe(":12345", router))
}
