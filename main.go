package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
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

type Customer struct {
	ID                primitive.ObjectID `json:"_id,omitempty" bson:"_id,omitempty"`
	FirstName         string             `json:"firstName,omitempty" bson:"firstName,omitempty"`
	Username          string             `json:"username,omitempty" bson:"username,omitempty"`
	TelegramUserId    string             `json:"telegramUserId,omitempty" bson:"telegramUserId,omitempty"`
	ShopifyCustomerId string             `json:"shopifyCustomerId,omitempty" bson:"shopifyCustomerId,omitempty"`
	Score             float64            `json:"score" bson:"score"`
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
var drinkPriceRule = "1716197753141"
var foodPriceRule = "1716313260341"

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

	server := os.Getenv("SERVER")
	if server == "" {
		log.Fatalf("SERVER is not set in .env file")
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
	b.Handle("/get_customer_info", GetCustomerInfoHandler)
	b.Handle("/redeem_points", RedeemPointsHandler)
	b.Handle(&telebot.InlineButton{Unique: "exchange_drink"}, ExchangeDrinkHandler)
	b.Handle(&telebot.InlineButton{Unique: "exchange_food"}, ExchangeFoodHandler)

	go func() {
		startTime := time.Now()
		fmt.Printf("Starting Telegram bot at %s...\n", startTime.Format(time.RFC3339))
		b.Start()
	}()

	router := mux.NewRouter()
	router.HandleFunc("/api/nuke", NukeEndpoint).Methods("GET")
	router.HandleFunc("/api/customers", GetCustomersEndpoint).Methods("GET")
	router.HandleFunc("/api/histories", GetHistoriesEndpoint).Methods("GET")
	router.HandleFunc("/api/shopify/webhook", func(w http.ResponseWriter, r *http.Request) {
		topic := r.Header.Get("X-Shopify-Topic")
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "Cannot read body", http.StatusOK)
			return
		}

		var result map[string]interface{}
		if err := json.Unmarshal(body, &result); err != nil {
			http.Error(w, "Cannot unmarshal JSON", http.StatusOK)
			return
		}

		fmt.Println("topic", topic)

		if topic == "orders/create" {
			customer, ok := result["customer"].(map[string]interface{})
			if !ok {
				http.Error(w, "Customer field is missing", http.StatusOK)
				return
			}

			customerID, ok := customer["id"].(float64)
			if !ok {
				http.Error(w, "Customer ID is missing", http.StatusOK)
				return
			}

			sid := fmt.Sprintf("%.0f", customerID)
			fmt.Println("Customer ID: ", sid)

			collection := client.Database(dbName).Collection(customerCollection)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			var c Customer
			collection.FindOne(ctx, bson.M{"shopifyCustomerId": sid}).Decode(&c)
			fmt.Println("c: ", c)

			if c.TelegramUserId != "" {
				var score float64 = 2
				newScore := c.Score + score

				collection.UpdateOne(
					context.Background(),
					bson.M{"_id": c.ID},
					bson.M{"$set": bson.M{"score": newScore}},
				)

				historyCollection := client.Database(dbName).Collection(historyCollection)
				historyRecord := History{
					CustomerID: c.ID.String(),
					Score:      score,
					Type:       "buy",
					Timestamp:  time.Now().Unix(),
				}
				historyCollection.InsertOne(context.Background(), historyRecord)

				sendMessage(b, c.TelegramUserId, "You are increased "+fmt.Sprint(score)+" points")
			}
		}

		if topic == "orders/updated" {
			//
		}

		w.WriteHeader(http.StatusOK)
	}).Methods("POST")
	log.Fatal(http.ListenAndServe(":12345", router))
}

func sendMessage(b *telebot.Bot, userID string, message string) error {
	chatID, err := strconv.ParseInt(userID, 10, 64)
	if err != nil {
		return err
	}
	_, err = b.Send(&telebot.Chat{ID: chatID}, message)
	return err
}

func TextHandler(c telebot.Context) error {
	u := c.Sender()
	t := c.Text()
	fmt.Println("Received message:", u.FirstName, t)
	return c.Send("There are commands I support:\nGet customer info: /get_customer_info\nRedeem points: /redeem_points")
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
			token := os.Getenv("SHOPIFY_TOKEN")
			storeURL := os.Getenv("SHOPIFY_STORE_URL")
			apiEndpoint := fmt.Sprintf("%s/admin/api/2023-07/customers.json", storeURL)

			payload := map[string]interface{}{
				"customer": map[string]interface{}{
					"first_name": user.FirstName,
					"last_name":  tid,
					"email":      fmt.Sprintf("%s@yopmail.%s", tid, os.Getenv("SERVER")),
				},
			}

			data, err := json.Marshal(payload)
			if err != nil {
				log.Fatalf("Error marshalling customer data: %v", err)
			}

			req, err := http.NewRequest("POST", apiEndpoint, bytes.NewBuffer(data))
			if err != nil {
				log.Fatalf("Error creating request: %v", err)
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Shopify-Access-Token", token)

			httpClient := &http.Client{}
			resp, err := httpClient.Do(req)
			if err != nil {
				log.Fatalf("Error sending request: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusCreated {
				log.Fatalf("Failed to create customer: %s", resp.Status)
			}

			var responseBody map[string]interface{}
			if err := json.NewDecoder(resp.Body).Decode(&responseBody); err != nil {
				log.Fatalf("Error reading response body: %v", err)
			}

			customerID := responseBody["customer"].(map[string]interface{})["id"].(float64)
			sid := fmt.Sprintf("%.0f", customerID)

			cp := Customer{
				FirstName:         user.FirstName,
				Username:          user.Username,
				TelegramUserId:    tid,
				ShopifyCustomerId: sid,
				Score:             0,
			}
			collection.InsertOne(ctx, cp)
			fmt.Println("Application have new customer", cp)
		} else {
			log.Fatal(err)
		}
	}

	return c.Send("Hello " + user.FirstName)
}

func GetCustomerInfoHandler(c tele.Context) error {
	user := c.Sender()
	tid := fmt.Sprint(user.ID)

	collection := client.Database(dbName).Collection(customerCollection)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var customer Customer
	collection.FindOne(ctx, bson.M{"telegramUserId": tid}).Decode(&customer)

	c.Send("telegram user ID: %s\nshopify user ID: %s", tid, customer.ShopifyCustomerId)

	qr, err := qrcode.New(tid, qrcode.Medium)
	if err != nil {
		return c.Send("Failed to generate QR code")
	}

	var buffer bytes.Buffer
	qr.Write(200, &buffer)
	imageFile := telebot.File{
		FileReader: bytes.NewReader(buffer.Bytes()),
	}
	photo := &telebot.Photo{
		File:    imageFile,
		Caption: fmt.Sprintf("name: %s\ntelegram user ID: %s\nshopify user ID: %s", user.FirstName, tid, customer.ShopifyCustomerId),
	}

	fmt.Println("get_customer_info", user.FirstName)
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
		Text:   fmt.Sprintf("Exchange free food (%.0f points)", foodPoint),
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
	code := generateRandomCode()

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

	token := os.Getenv("SHOPIFY_TOKEN")
	storeURL := os.Getenv("SHOPIFY_STORE_URL")
	apiEndpoint := fmt.Sprintf("%s/admin/api/2023-07/price_rules/%s/discount_codes.json", storeURL, drinkPriceRule)

	payload := map[string]interface{}{
		"discount_code": map[string]interface{}{
			"code": code,
		},
	}

	data, err := json.Marshal(payload)
	if err != nil {
		log.Fatalf("Error marshalling data: %v", err)
	}

	req, err := http.NewRequest("POST", apiEndpoint, bytes.NewBuffer(data))
	if err != nil {
		log.Fatalf("Error creating request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Shopify-Access-Token", token)

	httpClient := &http.Client{}
	resp, err := httpClient.Do(req)
	if err != nil {
		log.Fatalf("Error sending request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		log.Fatalf("Failed: %s", resp.Status)
	}

	var responseBody map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&responseBody); err != nil {
		log.Fatalf("Error reading response body: %v", err)
	}

	newScore := customer.Score - drinkPoint

	collection.UpdateOne(
		context.Background(),
		bson.M{"_id": customer.ID},
		bson.M{"$set": bson.M{"score": newScore}},
	)

	historyCollection := client.Database(dbName).Collection(historyCollection)
	historyRecord := History{
		CustomerID: customer.ID.String(),
		Score:      drinkPoint,
		Type:       "redeem",
		Timestamp:  time.Now().Unix(),
	}
	historyCollection.InsertOne(context.Background(), historyRecord)

	fmt.Println("exchange_drink", customer.FirstName)
	return c.Send("Exchange free drink for " + customer.FirstName + " - code: " + code)
}

func ExchangeFoodHandler(c telebot.Context) error {
	code := generateRandomCode()

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

	if customer.Score < foodPoint {
		return c.Send("Not enough points to exchange food. Let's order more 🚀")
	}

	token := os.Getenv("SHOPIFY_TOKEN")
	storeURL := os.Getenv("SHOPIFY_STORE_URL")
	apiEndpoint := fmt.Sprintf("%s/admin/api/2023-07/price_rules/%s/discount_codes.json", storeURL, foodPriceRule)

	payload := map[string]interface{}{
		"discount_code": map[string]interface{}{
			"code": code,
		},
	}

	data, err := json.Marshal(payload)
	if err != nil {
		log.Fatalf("Error marshalling data: %v", err)
	}

	req, err := http.NewRequest("POST", apiEndpoint, bytes.NewBuffer(data))
	if err != nil {
		log.Fatalf("Error creating request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Shopify-Access-Token", token)

	httpClient := &http.Client{}
	resp, err := httpClient.Do(req)
	if err != nil {
		log.Fatalf("Error sending request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		log.Fatalf("Failed: %s", resp.Status)
	}

	var responseBody map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&responseBody); err != nil {
		log.Fatalf("Error reading response body: %v", err)
	}

	newScore := customer.Score - foodPoint

	collection.UpdateOne(
		context.Background(),
		bson.M{"_id": customer.ID},
		bson.M{"$set": bson.M{"score": newScore}},
	)

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
