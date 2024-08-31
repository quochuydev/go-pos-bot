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

type Customer struct {
	ID             primitive.ObjectID `json:"_id,omitempty" bson:"_id,omitempty"`
	FirstName      string             `json:"firstName,omitempty" bson:"firstName,omitempty"`
	Username       string             `json:"username,omitempty" bson:"username,omitempty"`
	TelegramUserId string             `json:"telegramUserId,omitempty" bson:"telegramUserId,omitempty"`
	Score          float64            `json:"score" bson:"score"`
}

type History struct {
	CustomerID string  `json:"customer_id" bson:"customer_id"`
	Score      float64 `json:"score" bson:"score"`
	Type       string  `json:"type" bson:"type"`
	Timestamp  int64   `json:"timestamp" bson:"timestamp"`
}

var dbName string = "pos"
var customerCollection string = "customer"
var historyCollection string = "history"
var drinkPoint float64 = 2
var foodPoint float64 = 4

type ShortCode struct {
	Code string `json:"code"`
}

type VerificationRequest struct {
	Score float64 `json:"score"`
	Code  string  `json:"code"`
}

func init() {
	rand.Seed(uint64(time.Now().UnixNano()))
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

	b.Handle("/start", StartHandler)
	b.Handle(telebot.OnText, TextHandler)
	b.Handle("/get_points_code", GetPointsCodeHandler)
	b.Handle("/redeem_points", RedeemPointsHandler)
	b.Handle(&telebot.InlineButton{Unique: "exchange_drink"}, ExchangeDrinkHandler)
	b.Handle(&telebot.InlineButton{Unique: "exchange_food"}, ExchangeFoodHandler)

	go func() {
		fmt.Println("Starting Telegram bot...")
		b.Start()
	}()

	router := mux.NewRouter()
	router.HandleFunc("/api/nuke", NukeEndpoint).Methods("GET")
	router.HandleFunc("/api/customers", GetCustomersEndpoint).Methods("GET")
	router.HandleFunc("/api/histories", GetHistoriesEndpoint).Methods("GET")
	router.HandleFunc("/api/qrcode/verify", VerifyCodeEndpoint).Methods("POST")
	log.Fatal(http.ListenAndServe(":12345", router))
}

func TextHandler(c telebot.Context) error {
	u := c.Sender()
	t := c.Text()
	fmt.Println("Received message:", u.FirstName, t)
	return c.Send("There are commands I support:\nGet points code: /get_points_code\nRedeem points: /redeem_points")
}

func generateRandomCode() string {
	return strconv.Itoa(100000 + rand.Intn(900000))
}

func GetCustomersEndpoint(response http.ResponseWriter, request *http.Request) {
	response.Header().Set("Content-Type", "application/json")
	var customers []Customer

	collection := client.Database(dbName).Collection(customerCollection)
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

func GetHistoriesEndpoint(response http.ResponseWriter, request *http.Request) {
	response.Header().Set("Content-Type", "application/json")
	var histories []History

	collection := client.Database(dbName).Collection(historyCollection)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cursor, err := collection.Find(ctx, bson.M{})
	if err != nil {
		log.Fatal(err)
	}
	defer cursor.Close(ctx)
	for cursor.Next(ctx) {
		var h History
		cursor.Decode(&h)
		histories = append(histories, h)
	}

	json.NewEncoder(response).Encode(histories)
}

func VerifyCodeEndpoint(response http.ResponseWriter, request *http.Request) {
	var req VerificationRequest
	if err := json.NewDecoder(request.Body).Decode(&req); err != nil {
		http.Error(response, "Invalid request body", http.StatusBadRequest)
		return
	}

	fmt.Println("req.Code", req.Code)
	fmt.Println("req.Score", req.Score)

	tid, exists := codeStore.Load(req.Code)

	if !exists {
		http.Error(response, "Invalid code", http.StatusBadRequest)
		return
	}

	if tid == nil || tid.(string) == "" {
		http.Error(response, "Invalid customer ID", http.StatusBadRequest)
		return
	}

	codeStore.Delete(req.Code)

	collection := client.Database(dbName).Collection(customerCollection)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var customer Customer
	collection.FindOne(ctx, bson.M{"telegramUserId": tid.(string)}).Decode(&customer)

	newScore := customer.Score + req.Score
	collection.UpdateOne(
		context.Background(),
		bson.M{"_id": customer.ID},
		bson.M{"$set": bson.M{"score": newScore}},
	)

	historyCollection := client.Database(dbName).Collection(historyCollection)
	historyRecord := History{
		CustomerID: customer.ID.String(),
		Score:      req.Score,
		Type:       "buy",
		Timestamp:  time.Now().Unix(),
	}
	historyCollection.InsertOne(context.Background(), historyRecord)

	response.WriteHeader(http.StatusOK)
	json.NewEncoder(response).Encode(map[string]string{"telegramUserId": tid.(string)})
}

func StartHandler(c tele.Context) error {
	user := c.Sender()
	tid := fmt.Sprint(user.ID)

	collection := client.Database(dbName).Collection(customerCollection)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var customer Customer
	err := collection.FindOne(ctx, bson.M{"telegramUserId": tid}).Decode(&customer)

	if err != nil {
		if err == mongo.ErrNoDocuments {
			cp := Customer{
				FirstName:      user.FirstName,
				Username:       user.Username,
				TelegramUserId: tid,
				Score:          0,
			}
			collection.InsertOne(ctx, cp)
			fmt.Println("Application have new customer", cp)
		} else {
			log.Fatal(err)
		}
	}

	return c.Send("Hello " + user.FirstName)
}

func GetPointsCodeHandler(c tele.Context) error {
	user := c.Sender()
	code := generateRandomCode()
	codeStore.Store(code, fmt.Sprint(user.ID))
	c.Send("Points code: " + code)

	qr, err := qrcode.New(code, qrcode.Medium)
	if err != nil {
		return c.Send("Failed to generate QR code")
	}

	var buffer bytes.Buffer
	qr.Write(200, &buffer)
	imageFile := telebot.File{
		FileReader: bytes.NewReader(buffer.Bytes()),
	}
	photo := &telebot.Photo{
		File: imageFile,
	}

	fmt.Println("get_points_code", user.FirstName)
	return c.Send(photo)
}

func RedeemPointsHandler(c tele.Context) error {
	user := c.Sender()
	collection := client.Database(dbName).Collection(customerCollection)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var customer Customer
	collection.FindOne(ctx, bson.M{"telegramUserId": fmt.Sprint(user.ID)}).Decode(&customer)
	score := fmt.Sprint(customer.Score)

	if customer.Score < drinkPoint {
		m := "You have: " + score + " points.\n\nLet's order to get points"
		fmt.Println("redeem_points", customer.FirstName, score)
		return c.Send(m)
	}

	exchangeDrinkBtn := telebot.InlineButton{
		Unique: "exchange_drink",
		Text:   fmt.Sprintf("Exchange free drink (%.0f points)", drinkPoint),
	}
	exchangeFoodBtn := telebot.InlineButton{
		Unique: "exchange_food",
		Text:   fmt.Sprintf("Exchange free drink (%.0f points)", foodPoint),
	}
	replyMarkup := &telebot.ReplyMarkup{
		InlineKeyboard: [][]telebot.InlineButton{
			{exchangeDrinkBtn},
			{exchangeFoodBtn},
		},
	}

	m := "You have: " + score + " points.\n\nExchange points to get free drink"
	fmt.Println("redeem_points", customer.FirstName, score)
	return c.Send(m, replyMarkup)
}

func ExchangeDrinkHandler(c telebot.Context) error {
	updatedMarkup := &telebot.ReplyMarkup{
		InlineKeyboard: [][]telebot.InlineButton{},
	}
	err := c.Edit(c.Message().Text, updatedMarkup)
	if err != nil {
		return err
	}

	user := c.Sender()
	collection := client.Database(dbName).Collection(customerCollection)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var customer Customer
	collection.FindOne(ctx, bson.M{"telegramUserId": fmt.Sprint(user.ID)}).Decode(&customer)

	historyCollection := client.Database(dbName).Collection(historyCollection)
	historyRecord := History{
		CustomerID: customer.ID.String(),
		Score:      drinkPoint,
		Type:       "redeem",
		Timestamp:  time.Now().Unix(),
	}
	historyCollection.InsertOne(context.Background(), historyRecord)

	fmt.Println("exchange_drink", customer.FirstName)
	return c.Send("Exchange free drink for " + customer.FirstName)
}

func ExchangeFoodHandler(c telebot.Context) error {
	updatedMarkup := &telebot.ReplyMarkup{
		InlineKeyboard: [][]telebot.InlineButton{},
	}
	err := c.Edit(c.Message().Text, updatedMarkup)
	if err != nil {
		return err
	}

	user := c.Sender()
	collection := client.Database(dbName).Collection(customerCollection)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var customer Customer
	collection.FindOne(ctx, bson.M{"telegramUserId": fmt.Sprint(user.ID)}).Decode(&customer)

	historyCollection := client.Database(dbName).Collection(historyCollection)
	historyRecord := History{
		CustomerID: customer.ID.String(),
		Score:      foodPoint,
		Type:       "redeem",
		Timestamp:  time.Now().Unix(),
	}
	historyCollection.InsertOne(context.Background(), historyRecord)

	fmt.Println("exchange_food", customer.FirstName)
	return c.Send("Exchange free food for " + customer.FirstName)
}

func NukeEndpoint(response http.ResponseWriter, request *http.Request) {
	client.Database(dbName).Drop(context.Background())
}
