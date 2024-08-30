## DIY

### Manual create Telegram bot

#### Test BOT

<img src="docs/bot.jpg" alt="t.me/Goposv1Bot" width="200"/>

### Setup free MongoDB Atlas

#### - Register a free mongo cluster

https://cloud.mongodb.com

#### - Create a new cluster

![MongoDB Atlas](docs/mongodb-atlas.png)

#### - Create a new cluster

![MongoDB Atlas Network Access](docs/mongodb-network-access.png)

### Deploy Golang in AWS EC2

OS: AWS Linux

```
uname -s

uname -m
```

If the outputs match Linux and x86_64, then the corresponding values in Go would be:

```
GOOS=linux GOARCH=amd64 go build -o myapp
```

### Integrate to POS application

#### Test APIs

```sh
curl -X POST -H "Content-Type: application/json" -d '{"code": "889351", "score": 1.2}' http://localhost:12345/api/qrcode/verify

curl http://localhost:12345/api/customers
```
